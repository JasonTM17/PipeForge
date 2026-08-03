[CmdletBinding()]
param(
    [string]$BaseUrl = "http://localhost:58080",
    [ValidateRange(4, 20)]
    [int]$JobCount = 4,
    [int]$TimeoutSeconds = 90
)

$ErrorActionPreference = "Stop"

function Invoke-DatabaseScalar {
    param([string]$Query)

    $rows = @(Invoke-DatabaseRows -Query $Query)
    if ($rows.Count -eq 0) {
        throw "database verification returned no value"
    }
    return $rows[-1]
}

function Invoke-DatabaseRows {
    param([string]$Query)

    $output = $Query | & docker compose exec -T postgres `
        sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tA'
    if ($LASTEXITCODE -ne 0) {
        throw "database verification failed"
    }
    return @($output | ForEach-Object { $_.Trim() } | Where-Object { $_ })
}

$activeWorkers = [int](Invoke-DatabaseScalar -Query @"
SELECT COUNT(*)
FROM processing_workers
WHERE status IN ('READY', 'BUSY')
  AND last_heartbeat_at >= NOW() - INTERVAL '2 minutes';
"@)
if ($activeWorkers -lt 2) {
    throw "multi-worker verification requires at least two active workers (actual: $activeWorkers)"
}

$verificationJobs = @()
$results = @()
$e2eScript = Join-Path $PSScriptRoot "e2e-compose.ps1"
try {
    for ($index = 0; $index -lt $JobCount; $index++) {
        $verificationJobs += Start-Job -ScriptBlock {
            param($ScriptPath, $Url, $Timeout)
            & $ScriptPath -BaseUrl $Url -TimeoutSeconds $Timeout
        } -ArgumentList $e2eScript, $BaseUrl, $TimeoutSeconds
    }
    $verificationJobs | Wait-Job -Timeout ($TimeoutSeconds + 60) | Out-Null
    foreach ($verificationJob in $verificationJobs) {
        if ($verificationJob.State -ne "Completed") {
            throw "concurrent end-to-end job did not complete: $($verificationJob.State)"
        }
        $output = @(Receive-Job -Job $verificationJob)
        $json = $output | Where-Object { [string]$_ -match '^\s*\{' } | Select-Object -Last 1
        if ($null -eq $json) {
            throw "concurrent end-to-end job returned no JSON result"
        }
        $results += $json | ConvertFrom-Json
    }
}
finally {
    $verificationJobs | Remove-Job -Force -ErrorAction SilentlyContinue
}

$canonicalJobIds = @($results | ForEach-Object {
    $parsed = [Guid]::Empty
    if (-not [Guid]::TryParse([string]$_.JobId, [ref]$parsed)) {
        throw "end-to-end result returned an invalid job ID"
    }
    $parsed.ToString()
})
$jobList = ($canonicalJobIds | ForEach-Object { "'$_'::uuid" }) -join ","
$assignedWorkerIds = @(Invoke-DatabaseRows -Query @"
SELECT DISTINCT worker_id::text
FROM job_attempts
WHERE job_id IN ($jobList)
  AND state = 'SUCCEEDED'
  AND worker_id IS NOT NULL
ORDER BY worker_id::text;
"@)
if ($assignedWorkerIds.Count -lt 2) {
    throw "scheduler did not distribute verified jobs across two workers (actual: $($assignedWorkerIds.Count))"
}

$bindingLines = @(& docker compose exec -T rabbitmq rabbitmqctl -q `
    list_bindings source_name destination_name routing_key)
if ($LASTEXITCODE -ne 0) {
    throw "RabbitMQ binding verification failed"
}
$bindings = @($bindingLines | Select-Object -Skip 1 | ForEach-Object {
    $parts = [string]$_ -split "`t"
    if ($parts.Count -eq 3) {
        [pscustomobject]@{ Source = $parts[0]; Destination = $parts[1]; RoutingKey = $parts[2] }
    }
})
foreach ($workerId in $assignedWorkerIds) {
    foreach ($route in @(
        @{ Queue = "processing.jobs.$workerId"; Key = "processing.job.requested.$workerId" },
        @{ Queue = "processing.cancellations.$workerId"; Key = "processing.job.cancel-requested.$workerId" }
    )) {
        $matching = @($bindings | Where-Object {
            $_.Source -eq "pipeforge.commands" -and
            $_.Destination -eq $route.Queue -and
            $_.RoutingKey -eq $route.Key
        })
        if ($matching.Count -ne 1) {
            throw "worker queue has a missing or ambiguous targeted binding: $($route.Queue)"
        }
        $wrong = @($bindings | Where-Object {
            $_.Source -eq "pipeforge.commands" -and
            $_.Destination -eq $route.Queue -and
            $_.RoutingKey -ne $route.Key
        })
        if ($wrong.Count -ne 0) {
            throw "worker queue accepts another worker's routing key: $($route.Queue)"
        }
    }
}

$queueLines = @(& docker compose exec -T rabbitmq rabbitmqctl -q list_queues name consumers)
if ($LASTEXITCODE -ne 0) {
    throw "RabbitMQ consumer verification failed"
}
foreach ($legacyQueue in @("processing.jobs", "processing.cancellations")) {
    $line = $queueLines | Where-Object { [string]$_ -match "^$([regex]::Escape($legacyQueue))`t" }
    if ($null -eq $line) {
        throw "legacy queue is missing from the declared cutover topology: $legacyQueue"
    }
    $consumers = [int](([string]$line -split "`t")[-1])
    if ($consumers -ne 0) {
        throw "legacy compatibility must be disabled for the default multi-worker proof: $legacyQueue has $consumers consumer(s)"
    }
}

$cancellationOutput = @(& $e2eScript -BaseUrl $BaseUrl -TimeoutSeconds $TimeoutSeconds `
    -VerifyCancellation)
$cancellationJson = $cancellationOutput |
    Where-Object { [string]$_ -match '^\s*\{' } |
    Select-Object -Last 1
if ($null -eq $cancellationJson) {
    throw "targeted cancellation probe returned no JSON result"
}
$cancellationResult = $cancellationJson | ConvertFrom-Json
if ($cancellationResult.FinalState -ne "CANCELLED" -or -not $cancellationResult.CancellationRequested) {
    throw "targeted cancellation probe did not complete safely"
}

[pscustomobject]@{
    ActiveWorkers = $activeWorkers
    SubmittedJobs = $results.Count
    AssignedWorkers = $assignedWorkerIds.Count
    AssignedWorkerIds = $assignedWorkerIds
    TargetedBindingsVerified = $true
    LegacySharedQueueConsumers = 0
    CancellationJobId = $cancellationResult.JobId
    CancellationState = $cancellationResult.FinalState
    JobIds = $canonicalJobIds
    FinalStates = @($results.FinalState | Sort-Object -Unique)
} | ConvertTo-Json -Compress
