<#
.SYNOPSIS
  将本地 InspectAI MySQL 数据库推送到生产服务器并覆盖生产库。

.DESCRIPTION
  流程：
    1. 从本地 MYSQL_DSN 导出数据库。
    2. 上传 SQL 到服务器项目目录。
    3. 上传并调用 scripts/import-db-prod.sh。
    4. 服务器先备份现有库，再 DROP/CREATE 目标库并导入本地数据。

  默认会要求二次确认；如需无人值守，显式加 -Force。
  本脚本不会打印数据库密码。

.EXAMPLE
  .\scripts\push-db.ps1 -Server root@122.51.59.167

.EXAMPLE
  .\scripts\push-db.ps1 -Server root@122.51.59.167 -WithImages

.EXAMPLE
  .\scripts\push-db.ps1 -Server root@122.51.59.167 -LocalDsn "inspectai:pwd@tcp(127.0.0.1:3306)/inspectai?charset=utf8mb4&parseTime=false&loc=Local"
#>
param(
  [Parameter(Mandatory = $true)]
  [string]$Server,

  [string]$RemoteDir = "/opt/inspectai-src/inspectai",
  [string]$Compose = "docker-compose.prod.yml",
  [string]$EnvFile = ".env.prod",
  [string]$RemoteDbName = "",

  [string]$LocalDsn = "",
  [string]$MySqlDumpPath = "",

  [switch]$WithImages,
  [string]$AppStorageVolume = "inspectai_app_storage",

  [switch]$NoServerBackup,
  [switch]$NoRestartBackend,
  [switch]$Force
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$Stamp = Get-Date -Format "yyyyMMdd_HHmmss"

function Quote-Sh([string]$Value) {
  return "'" + $Value.Replace("'", '''"''"''') + "'"
}

function Get-EnvValueFromFile([string]$Path, [string]$Key, [switch]$Secure) {
  if (-not (Test-Path -LiteralPath $Path)) { return $null }
  foreach ($line in Get-Content -LiteralPath $Path -Encoding UTF8) {
    $t = $line.Trim()
    if (-not $t -or $t.StartsWith("#")) { continue }
    $idx = $t.IndexOf("=")
    if ($idx -le 0) { continue }
    if ($t.Substring(0, $idx).Trim() -ne $Key) { continue }
    $value = $t.Substring($idx + 1).Trim().Trim('"').Trim("'")
    if (-not $Secure) { return $value }
    $ss = ConvertTo-SecureString -String $value
    $b = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($ss)
    try {
      return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($b)
    } finally {
      [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($b)
    }
  }
  return $null
}

function Resolve-LocalDsn {
  if ($LocalDsn) { return $LocalDsn }
  if ($env:MYSQL_DSN) { return $env:MYSQL_DSN }

  $secure = Join-Path $Root ".env.secure"
  $fromSecure = Get-EnvValueFromFile -Path $secure -Key "MYSQL_DSN" -Secure
  if ($fromSecure) { return $fromSecure }

  $plain = Join-Path $Root ".env"
  $fromPlain = Get-EnvValueFromFile -Path $plain -Key "MYSQL_DSN"
  if ($fromPlain) { return $fromPlain }

  throw "未找到本地 MYSQL_DSN。请用 -LocalDsn 传入，或设置环境变量 MYSQL_DSN，或在 .env/.env.secure 中配置 MYSQL_DSN。"
}

function Resolve-MySqlDump {
  if ($MySqlDumpPath) {
    if (Test-Path -LiteralPath $MySqlDumpPath) { return $MySqlDumpPath }
    throw "找不到指定的 mysqldump：$MySqlDumpPath"
  }
  if ($env:MYSQLDUMP -and (Test-Path -LiteralPath $env:MYSQLDUMP)) { return $env:MYSQLDUMP }
  $cmd = Get-Command mysqldump.exe -ErrorAction SilentlyContinue
  if ($cmd) { return $cmd.Source }
  $cmd = Get-Command mysqldump -ErrorAction SilentlyContinue
  if ($cmd) { return $cmd.Source }

  $candidates = @(
    "D:\mysql\mysql-8.0.42-winx64\bin\mysqldump.exe",
    "C:\Program Files\MySQL\MySQL Server 8.0\bin\mysqldump.exe"
  )
  foreach ($candidate in $candidates) {
    if (Test-Path -LiteralPath $candidate) { return $candidate }
  }
  throw "找不到 mysqldump。请安装 MySQL Client，或用 -MySqlDumpPath 指定 mysqldump.exe。"
}

function Parse-MySqlDsn([string]$Dsn) {
  if ($Dsn -notmatch '^(?<user>[^:]+):(?<pass>.*)@tcp\((?<host>[^:]+):(?<port>\d+)\)/(?<db>[^?]+)') {
    throw "无法解析 MYSQL_DSN，期望格式：user:password@tcp(host:3306)/database?charset=utf8mb4..."
  }
  return @{
    User = $Matches.user
    Password = $Matches.pass
    Host = $Matches.host
    Port = $Matches.port
    Database = $Matches.db
  }
}

$dsn = Resolve-LocalDsn
$cfg = Parse-MySqlDsn $dsn
$mysqldump = Resolve-MySqlDump

Write-Host "本地库     : $($cfg.User)@$($cfg.Host):$($cfg.Port)/$($cfg.Database)" -ForegroundColor Cyan
Write-Host "目标服务器 : $Server" -ForegroundColor Cyan
Write-Host "服务器目录 : $RemoteDir" -ForegroundColor Cyan
Write-Host "Compose    : $Compose / $EnvFile" -ForegroundColor Cyan
if ($RemoteDbName) { Write-Host "目标库     : $RemoteDbName" -ForegroundColor Cyan }
if ($WithImages) { Write-Host "同步图片   : storage/uploads -> Docker volume $AppStorageVolume" -ForegroundColor Cyan }

if (-not $Force) {
  Write-Host ""
  Write-Host "危险操作：即将覆盖服务器 MySQL 生产库。服务器会先自动备份，但请确认当前目标服务器无误。" -ForegroundColor Yellow
  $ans = Read-Host "继续请输入 OVERWRITE"
  if ($ans -ne "OVERWRITE") {
    Write-Host "已取消。" -ForegroundColor Yellow
    exit 1
  }
}

$tmpDir = Join-Path $env:TEMP "inspectai-db-push-$Stamp"
New-Item -ItemType Directory -Path $tmpDir | Out-Null
$dump = Join-Path $tmpDir "inspectai_local_$Stamp.sql"

try {
  Write-Host "导出本地数据库 -> $dump" -ForegroundColor Green
  $env:MYSQL_PWD = $cfg.Password
  try {
    & $mysqldump `
      "-u$($cfg.User)" `
      "-h$($cfg.Host)" `
      "-P$($cfg.Port)" `
      --default-character-set=utf8mb4 `
      --single-transaction `
      --routines `
      --triggers `
      --events `
      --hex-blob `
      --add-drop-table `
      --no-tablespaces `
      "--result-file=$dump" `
      $cfg.Database
  } finally {
    $env:MYSQL_PWD = $null
  }
  if ($LASTEXITCODE -ne 0) { throw "mysqldump 失败，退出码：$LASTEXITCODE" }
  if (-not (Test-Path -LiteralPath $dump) -or (Get-Item -LiteralPath $dump).Length -lt 1024) {
    throw "导出的 SQL 文件过小，已中止。"
  }
  $hash = (Get-FileHash -LiteralPath $dump -Algorithm SHA256).Hash
  Write-Host "导出完成，SHA256: $hash" -ForegroundColor DarkGray

  $remoteSqlName = "_migrate_local_$Stamp.sql"
  $remoteSql = "$RemoteDir/$remoteSqlName"
  Write-Host "上传 SQL -> ${Server}:$remoteSql" -ForegroundColor Green
  & scp $dump "${Server}:$remoteSql"
  if ($LASTEXITCODE -ne 0) { throw "上传 SQL 失败。" }

  $localImportScript = Join-Path $PSScriptRoot "import-db-prod.sh"
  if (-not (Test-Path -LiteralPath $localImportScript)) {
    throw "缺少服务器导入脚本：$localImportScript"
  }
  Write-Host "上传服务器导入脚本 -> ${Server}:$RemoteDir/scripts/import-db-prod.sh" -ForegroundColor Green
  & scp $localImportScript "${Server}:$RemoteDir/scripts/import-db-prod.sh"
  if ($LASTEXITCODE -ne 0) { throw "上传导入脚本失败。" }

  $args = @(
    "set -e",
    "cd $(Quote-Sh $RemoteDir)",
    "chmod +x scripts/import-db-prod.sh",
    "bash scripts/import-db-prod.sh --file $(Quote-Sh $remoteSqlName) --compose $(Quote-Sh $Compose) --env-file $(Quote-Sh $EnvFile) --yes"
  )
  if ($RemoteDbName) { $args[3] += " --db-name $(Quote-Sh $RemoteDbName)" }
  if ($NoServerBackup) { $args[3] += " --no-backup" }
  if ($NoRestartBackend) { $args[3] += " --no-restart-backend" }

  Write-Host "服务器备份并覆盖导入中..." -ForegroundColor Green
  & ssh $Server ($args -join "; ")
  if ($LASTEXITCODE -ne 0) { throw "服务器导入失败。若已完成备份，请到服务器 backups/db/ 查找回滚文件。" }

  if ($WithImages) {
    $uploads = Join-Path $Root "storage\uploads"
    if (-not (Test-Path -LiteralPath $uploads)) {
      Write-Host "本地没有 storage/uploads，跳过图片同步。" -ForegroundColor Yellow
    } else {
      $tar = Join-Path $tmpDir "inspectai_uploads_$Stamp.tgz"
      Write-Host "打包上传图片 -> $tar" -ForegroundColor Green
      & tar -czf $tar -C (Join-Path $Root "storage") uploads
      if ($LASTEXITCODE -ne 0) { throw "打包图片失败。" }
      & scp $tar "${Server}:$RemoteDir/_migrate_uploads_$Stamp.tgz"
      if ($LASTEXITCODE -ne 0) { throw "上传图片包失败。" }
      $imgCmd = @(
        "set -e",
        "cd $(Quote-Sh $RemoteDir)",
        "docker run --rm -v ${AppStorageVolume}:/data -v ${RemoteDir}:/backup alpine sh -c $(Quote-Sh "tar xzf /backup/_migrate_uploads_$Stamp.tgz -C /data")",
        "rm -f $(Quote-Sh "_migrate_uploads_$Stamp.tgz")"
      ) -join "; "
      Write-Host "服务器同步图片中..." -ForegroundColor Green
      & ssh $Server $imgCmd
      if ($LASTEXITCODE -ne 0) { throw "服务器同步图片失败。" }
    }
  }

  Write-Host ""
  Write-Host "完成：服务器数据库已覆盖为本地数据。" -ForegroundColor Green
  Write-Host "服务器备份目录：$RemoteDir/backups/db" -ForegroundColor DarkGray
} finally {
  Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
