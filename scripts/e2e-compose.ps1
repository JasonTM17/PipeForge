[CmdletBinding()]
param(
    [string]$BaseUrl = "http://localhost:58080",
    [int]$TimeoutSeconds = 90,
    [switch]$VerifyCancellation,
    [ValidateRange(100000, 2000000)]
    [int]$CancellationRows = 750000
)

$ErrorActionPreference = "Stop"

function Invoke-JsonRequest {
    param(
        [ValidateSet("Get", "Post", "Delete")]
        [string]$Method,
        [string]$Path,
        [string]$Token,
        [object]$Body
    )

    $headers = @{}
    if ($Token) {
        $headers.Authorization = "Bearer $Token"
    }

    $request = @{
        Method      = $Method
        Uri         = "$BaseUrl$Path"
        Headers     = $headers
        ErrorAction = "Stop"
    }
    if ($null -ne $Body) {
        $request.Body = ($Body | ConvertTo-Json -Depth 8 -Compress)
        $request.ContentType = "application/json"
    }
    return Invoke-RestMethod @request
}

function Assert-Equal {
    param([object]$Actual, [object]$Expected, [string]$Message)
    if ($Actual -ne $Expected) {
        throw "$Message (actual: $Actual, expected: $Expected)"
    }
}

$suffix = [Guid]::NewGuid().ToString("N")
$email = "e2e-$suffix@example.test"
$password = "PipeForge-local-e2e-2026!"
$csvPath = Join-Path ([System.IO.Path]::GetTempPath()) "pipeforge-e2e-$suffix.csv"
$downloadPath = Join-Path ([System.IO.Path]::GetTempPath()) "pipeforge-e2e-$suffix-artifact.json"
$curlCommand = Get-Command curl.exe, curl -CommandType Application -ErrorAction SilentlyContinue |
    Select-Object -First 1
if ($null -eq $curlCommand) {
    throw "the local E2E requires the curl command-line client"
}

try {
    if ($VerifyCancellation) {
        $utf8WithoutBom = [System.Text.UTF8Encoding]::new($false)
        $writer = [System.IO.StreamWriter]::new($csvPath, $false, $utf8WithoutBom)
        try {
            $writer.WriteLine("customer_id,amount,email")
            for ($row = 1; $row -le $CancellationRows; $row++) {
                $writer.WriteLine("$row,$($row % 1000),user-$row@example.test")
            }
        }
        finally {
            $writer.Dispose()
        }
    }
    else {
        @"
customer_id,amount,email
1,10.5,alice@example.test
2,11.0,
3,11.0,bob@example.test
4,9999.0,not-an-email
"@ | Set-Content -LiteralPath $csvPath -Encoding UTF8
    }

    $tokens = Invoke-JsonRequest -Method Post -Path "/v1/auth/register" -Body @{
        email = $email
        password = $password
    }
    if (-not $tokens.accessToken) {
        throw "registration did not return an access token"
    }

    $dataset = Invoke-JsonRequest -Method Post -Path "/v1/datasets" -Token $tokens.accessToken -Body @{
        name = "compose-e2e-$suffix"
        description = "Disposable local end-to-end fixture"
    }

    $uploadBody = & $curlCommand.Source --fail-with-body --silent --show-error -X POST `
        "$BaseUrl/v1/datasets/$($dataset.id)/versions" `
        -H "Authorization: Bearer $($tokens.accessToken)" `
        -H "X-Filename: fixture.csv" `
        -H "Content-Type: text/csv" `
        --data-binary "@$csvPath"
    if ($LASTEXITCODE -ne 0) {
        throw "dataset version upload failed"
    }
    $version = $uploadBody | ConvertFrom-Json
    Assert-Equal $version.state "AVAILABLE" "dataset version was not made available"

    $job = Invoke-JsonRequest -Method Post -Path "/api/v1/datasets/$($version.id)/jobs" -Token $tokens.accessToken -Body @{
        operations = @(
            @{ type = "PROFILE_DATASET"; config = @{} },
            @{ type = "CHECK_MISSING_VALUES"; config = @{ columns = @("email") } },
            @{ type = "DETECT_OUTLIERS"; config = @{ column = "amount"; method = "IQR" } }
        )
        maxAttempts = 3
    }

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $cancelRequested = $false
    do {
        Start-Sleep -Milliseconds $(if ($VerifyCancellation) { 25 } else { 500 })
        $current = Invoke-JsonRequest -Method Get -Path "/api/v1/jobs/$($job.id)" -Token $tokens.accessToken
        if ($VerifyCancellation -and -not $cancelRequested -and $current.state -in @("LEASED", "RUNNING")) {
            $cancelled = Invoke-JsonRequest -Method Post -Path "/api/v1/jobs/$($job.id)/cancel" -Token $tokens.accessToken -Body @{
                reason = "multi-worker targeted cancellation probe"
            }
            $cancelRequested = $true
        }
        if ($current.state -in @("SUCCEEDED", "FAILED_PERMANENT", "DEAD_LETTERED", "CANCELLED")) {
            break
        }
    } while ((Get-Date) -lt $deadline)

    if ($VerifyCancellation) {
        if (-not $cancelRequested) {
            throw "cancellation probe never observed a leased or running job"
        }
        Assert-Equal $current.state "CANCELLED" "targeted cancellation did not reach the lease owner"
        [pscustomobject]@{
            DatasetId = $dataset.id
            VersionId = $version.id
            JobId = $job.id
            FinalState = $current.state
            CancellationRequested = $cancelRequested
            SourceRows = $CancellationRows
        } | ConvertTo-Json -Compress
        return
    }

    Assert-Equal $current.state "SUCCEEDED" "local end-to-end job did not succeed"
    $artifacts = Invoke-JsonRequest -Method Get -Path "/api/v1/jobs/$($job.id)/artifacts?page=1&pageSize=20" -Token $tokens.accessToken
    Assert-Equal $artifacts.total 3 "local end-to-end job did not publish three artifacts"

    $downloadStatus = & $curlCommand.Source --fail-with-body --silent --show-error `
        -o $downloadPath -w "%{http_code}" `
        "$BaseUrl/api/v1/artifacts/$($artifacts.items[0].id)/download" `
        -H "Authorization: Bearer $($tokens.accessToken)"
    if ($LASTEXITCODE -ne 0) {
        throw "artifact download failed"
    }
    Assert-Equal $downloadStatus "200" "artifact download failed"
    if ((Get-Item -LiteralPath $downloadPath).Length -lt 2) {
        throw "artifact download returned an empty body"
    }

    [pscustomobject]@{
        DatasetId = $dataset.id
        VersionId = $version.id
        JobId = $job.id
        FinalState = $current.state
        ArtifactKinds = @($artifacts.items | ForEach-Object { $_.kind } | Sort-Object)
        DownloadStatus = [int]$downloadStatus
    } | ConvertTo-Json -Compress
}
finally {
    Remove-Item -LiteralPath $csvPath -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $downloadPath -Force -ErrorAction SilentlyContinue
}
