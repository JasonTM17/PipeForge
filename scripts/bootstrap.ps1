[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath '.env')) {
    Copy-Item -LiteralPath '.env.example' -Destination '.env'
    Write-Output 'Created .env from .env.example; review local development values.'
}

& powershell -NoProfile -ExecutionPolicy Bypass -File 'scripts/validate-env.ps1' -Path '.env'
docker compose up -d postgres rabbitmq rabbitmq-init minio minio-init
docker compose ps
