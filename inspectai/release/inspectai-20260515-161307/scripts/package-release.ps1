$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$releaseDir = Join-Path $root "release"
$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$stage = Join-Path $releaseDir "inspectai-$stamp"
$zipPath = Join-Path $releaseDir "inspectai-deploy-$stamp.zip"

New-Item -ItemType Directory -Force -Path $releaseDir | Out-Null
if (Test-Path -LiteralPath $stage) {
  Remove-Item -LiteralPath $stage -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $stage | Out-Null

$items = @(
  "ai-service",
  "frontend",
  "go-backend",
  "nginx",
  "scripts",
  "docker-compose.prod.yml",
  ".dockerignore",
  ".env.prod.example",
  "README.md",
  "DEPLOY.md"
)

foreach ($item in $items) {
  $src = Join-Path $root $item
  if (-not (Test-Path -LiteralPath $src)) { continue }
  $dst = Join-Path $stage $item
  if ((Get-Item -LiteralPath $src).PSIsContainer) {
    Copy-Item -LiteralPath $src -Destination $dst -Recurse -Force
  } else {
    Copy-Item -LiteralPath $src -Destination $dst -Force
  }
}

$removePatterns = @(
  ".gocache",
  ".gotelemetry",
  "__pycache__",
  "*.pyc",
  "*.pid",
  "*.log",
  "*.exe",
  "*.exe~",
  "server.exe",
  "server-check.exe",
  "server_clean.exe"
)

foreach ($pattern in $removePatterns) {
  Get-ChildItem -LiteralPath $stage -Recurse -Force -Filter $pattern -ErrorAction SilentlyContinue |
    Remove-Item -Recurse -Force
}

Get-ChildItem -LiteralPath $stage -Recurse -Force -File -ErrorAction SilentlyContinue |
  Where-Object { $_.Name -match '\.go\.' } |
  Remove-Item -Force

if (Test-Path -LiteralPath $zipPath) {
  Remove-Item -LiteralPath $zipPath -Force
}
Compress-Archive -Path (Join-Path $stage "*") -DestinationPath $zipPath -Force

Write-Host "Release package created:"
Write-Host "  $zipPath"
Write-Host ""
Write-Host "Upload this zip to the server, unzip it, fill .env.prod, then run:"
Write-Host "  bash scripts/deploy-linux.sh"
