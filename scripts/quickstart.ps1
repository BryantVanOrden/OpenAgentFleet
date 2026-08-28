Write-Host "================================================================================" -ForegroundColor Cyan
Write-Host "                       ⚡ AGENTFLEET QUICKSTART BOOTSTRAP                       " -ForegroundColor Cyan
Write-Host "================================================================================" -ForegroundColor Cyan

# 1. Check Docker
if (Get-Command docker -ErrorAction SilentlyContinue) {
    Write-Host "✅ Docker found." -ForegroundColor Green
} else {
    Write-Host "⚠️  Docker not detected. Please install Docker Desktop to run sandboxed containers." -ForegroundColor Yellow
}

# 2. Check Python & SDK
if (Get-Command python -ErrorAction SilentlyContinue) {
    Write-Host "✅ Python found. Installing SDK in editable mode..." -ForegroundColor Green
    pip install -e sdk/python --quiet
    Write-Host "✅ fleetctl installed." -ForegroundColor Green
}

# 3. Prompt for Docker Compose launch
$choice = Read-Host "Start local Docker Compose production stack now? (y/n)"
if ($choice -eq 'y' -or $choice -eq 'Y') {
    Write-Host "🐳 Launching AgentFleet production stack..." -ForegroundColor Cyan
    docker compose up -d
    Write-Host "✅ AgentFleet is live at http://localhost:8080 (Admin Console: http://localhost:3000)" -ForegroundColor Green
} else {
    Write-Host "Run `docker compose up -d` whenever you are ready." -ForegroundColor Cyan
}
