$ErrorActionPreference = "Stop"

# 打包当前 inspectai 目录为一份干净的部署 zip，给 Linux 服务器手动 scp 用。
# 输出：release/inspectai-deploy-<时间戳>.zip
#
# 不包括：release/ samples/ test/ 截图 / *.exe / *.log / __pycache__ / storage/ /
#         .env / .env.prod / secrets/ / login-bg-options/ 等

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

# 白名单：只复制部署需要的目录 / 文件
$items = @(
  "ai-service",
  "admin-frontend",
  "frontend",
  "go-backend",
  "nginx",
  "scripts",
  "docs",
  "docker-compose.prod.yml",
  ".dockerignore",
  ".env.prod.example",
  ".gitattributes",
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

# 黑名单：在 stage 目录里递归清掉这些
$removePatterns = @(
  ".gocache",
  ".gotelemetry",
  "__pycache__",
  "*.pyc",
  "*.pid",
  "*.log",
  "*.exe",
  "*.exe~",
  "*.exe~*",
  "server.exe",
  "server-check.exe",
  "server_clean.exe",
  ".env",
  ".env.prod",
  ".env.secure",
  "secrets",
  "storage",
  "login-bg-options"
)

foreach ($pattern in $removePatterns) {
  Get-ChildItem -LiteralPath $stage -Recurse -Force -Filter $pattern -ErrorAction SilentlyContinue |
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
}

# 清掉 Go 源码里被改坏的 .go.<timestamp> 备份
Get-ChildItem -LiteralPath $stage -Recurse -Force -File -ErrorAction SilentlyContinue |
  Where-Object { $_.Name -match '\.go\.\d' } |
  Remove-Item -Force

# 删除 docs/ 下与部署无关的设计稿
$designDocs = Join-Path $stage "docs/ui-design"
if (Test-Path -LiteralPath $designDocs) {
  Remove-Item -LiteralPath $designDocs -Recurse -Force
}

# 体积检查
$stageSize = (Get-ChildItem -LiteralPath $stage -Recurse -File | Measure-Object Length -Sum).Sum
$stageSizeMB = [math]::Round($stageSize / 1MB, 2)
Write-Host "Stage 内容大小：$stageSizeMB MB"

if (Test-Path -LiteralPath $zipPath) {
  Remove-Item -LiteralPath $zipPath -Force
}
Compress-Archive -Path (Join-Path $stage "*") -DestinationPath $zipPath -Force

$zipSizeMB = [math]::Round((Get-Item $zipPath).Length / 1MB, 2)
Write-Host ""
Write-Host "✔ Release 包已生成（zip 压缩后 $zipSizeMB MB）："
Write-Host "  $zipPath"
Write-Host ""
Write-Host "上传到服务器后："
Write-Host "  1. unzip 解压"
Write-Host "  2. cp .env.prod.example .env.prod  并编辑（只填非敏感）"
Write-Host "  3. export DASHSCOPE_API_KEY=... 等敏感变量，跑 bash scripts/prepare-secrets.sh"
Write-Host "  4. bash scripts/deploy-linux.sh"
Write-Host ""
Write-Host "完整说明：docs/DEPLOY.md"
