<#
.SYNOPSIS
OpenAgentFleet 1-Click Interactive Quickstart & Bootstrap Script (Windows PowerShell)
#>

$Cyan = "Cyan"
$Green = "Green"
$Yellow = "Yellow"
$Red = "Red"

Write-Host @"
    ___                    __   ______ __            __ 
   /   | ____ _ ___  ____  / /_ / ____// /___   ___  / /_
  / /| |/ __ `// _ \/ __ \/ __// /_   / // _ \ / _ \/ __/
 / ___ / /_/ //  __/ / / / /_ / __/  / //  __//  __/ /_  
/_/  |_\__, / \___/_/ /_/\__//_/    /_/ \___/ \___/\__/  
      /____/                                             
"@ -ForegroundColor $Cyan

Write-Host "🚀 Autonomous Multi-Agent Desktop Fleet Orchestration Platform" -ForegroundColor $Cyan
Write-Host "==============================================================" -ForegroundColor $Cyan
Write-Host ""

# 1. Check Docker Daemon
Write-Host -NoNewline "🔍 Checking Docker installation and daemon... "
$dockerCmd = Get-Command docker -ErrorAction SilentlyContinue
if (-not $dockerCmd) {
    Write-Host "FAILED" -ForegroundColor $Red
    Write-Host "Error: Docker is not installed or not in PATH. Please install Docker Desktop for Windows." -ForegroundColor $Red
    exit 1
}

$dockerInfo = docker info 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "FAILED" -ForegroundColor $Red
    Write-Host "Error: Docker daemon is not running. Please start Docker Desktop." -ForegroundColor $Red
    exit 1
}
Write-Host "OK" -ForegroundColor $Green

# 2. Check or Generate .env Configuration
if (-not (Test-Path ".env")) {
    Write-Host "⚙️  No .env file found. Generating production-grade cryptographic secrets..." -ForegroundColor $Yellow

    function Get-RandomSecret([int]$bytes = 32) {
        $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
        $buffer = New-Object byte[] $bytes
        $rng.GetBytes($buffer)
        return -join ($buffer | ForEach-Object { "{0:x2}" -f $_ })
    }

    $MasterKey = Get-RandomSecret 32
    $JwtSecret = Get-RandomSecret 32
    $PgPass = (Get-RandomSecret 16).Substring(0, 20)

    $envContent = @"
# OpenAgentFleet Production Environment Configuration
PORT=8080
BASE_URL=http://localhost:8080
MASTER_KEY=$MasterKey
JWT_SECRET=$JwtSecret
DATABASE_URL=postgres://agentfleet:$PgPass@postgres:5432/agentfleet?sslmode=disable
POSTGRES_USER=agentfleet
POSTGRES_PASSWORD=$PgPass
POSTGRES_DB=agentfleet
HOST_GATEWAY=host.docker.internal
"@

    Set-Content -Path ".env" -Value $envContent -Encoding utf8
    Write-Host "✔ Created .env with sealed AES-256 MASTER_KEY and JWT_SECRET." -ForegroundColor $Green
} else {
    Write-Host "✔ Existing .env file found." -ForegroundColor $Green
}

# 3. Boot Full Fleet Stack
Write-Host ""
Write-Host "🐳 Starting OpenAgentFleet stack via Docker Compose..." -ForegroundColor $Cyan
docker compose up -d

Write-Host ""
Write-Host "==============================================================" -ForegroundColor $Green
Write-Host "🎉 OpenAgentFleet is up and running!" -ForegroundColor $Green
Write-Host "==============================================================" -ForegroundColor $Green
Write-Host "🖥️  Web Admin Console:   http://localhost:5173" -ForegroundColor $Cyan
Write-Host "⚡ Orchestrator API:     http://localhost:8080" -ForegroundColor $Cyan
Write-Host "📱 Health Endpoint:      http://localhost:8080/healthz" -ForegroundColor $Cyan
Write-Host ""
Write-Host "💡 Tip: Install the Python SDK and CLI tool:" -ForegroundColor $Yellow
Write-Host "   pip install -e ./sdk/python" -ForegroundColor $Yellow
Write-Host "   fleetctl doctor" -ForegroundColor $Yellow
Write-Host ""
