[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Path,
    [switch]$AllowMissing
)

$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath $Path)) {
    if ($AllowMissing) { exit 0 }
    throw "Environment file not found: $Path. Copy .env.example to .env first."
}

$required = @(
    'PIPEFORGE_ENV',
    'POSTGRES_DATABASE', 'POSTGRES_USER', 'POSTGRES_PASSWORD',
    'RABBITMQ_USER', 'RABBITMQ_PASSWORD',
    'MINIO_ACCESS_KEY', 'MINIO_SECRET_KEY'
)
$values = @{}
foreach ($line in Get-Content -LiteralPath $Path) {
    if ($line -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$') {
        $values[$Matches[1]] = $Matches[2].Trim().Trim('"').Trim("'")
    }
}

$missing = @($required | Where-Object { -not $values.ContainsKey($_) -or [string]::IsNullOrWhiteSpace($values[$_]) })
if ($missing.Count -gt 0) {
    throw "Missing required environment values: $($missing -join ', ')"
}

if ($values['PIPEFORGE_ENV'] -ne 'development') {
    Write-Warning 'Non-development environment selected; local Compose defaults are not production hardening.'
}

Write-Output "Environment validated: $Path"

