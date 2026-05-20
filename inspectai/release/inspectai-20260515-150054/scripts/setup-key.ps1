# 加密保存 DashScope API Key 到 .env.secure
# 用 Windows DPAPI（CurrentUser scope）加密，只有当前 Windows 账号能在本机解密
# 用法：
#   .\scripts\setup-key.ps1                  # 交互式输入，输入不回显
#   .\scripts\setup-key.ps1 -Key "sk-xxx"    # 命令行传入（不推荐，会进 PowerShell 历史）
#   .\scripts\setup-key.ps1 -Remove          # 删除已存的 key

param(
    [Parameter(Mandatory=$false)][string]$Key,
    [Parameter(Mandatory=$false)][string]$Name = "DASHSCOPE_API_KEY",
    [Parameter(Mandatory=$false)][switch]$Remove
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$secureFile = Join-Path $root ".env.secure"

# 读取已有内容（保留其他 key）
$existing = @()
if (Test-Path -LiteralPath $secureFile) {
    $existing = @(Get-Content -LiteralPath $secureFile | Where-Object {
        $_ -and -not $_.StartsWith("$Name=")
    })
}

if ($Remove) {
    if (-not (Test-Path -LiteralPath $secureFile) -or $existing.Count -eq (Get-Content -LiteralPath $secureFile).Count) {
        Write-Host "Key '$Name' not found in $secureFile"
        exit 0
    }
    Set-Content -LiteralPath $secureFile -Value $existing -Encoding UTF8
    Write-Host "Key '$Name' removed from $secureFile"
    exit 0
}

# 取明文：参数 > 交互输入
if ($Key) {
    $secure = ConvertTo-SecureString -String $Key -AsPlainText -Force
    # 立刻清除参数变量
    $Key = $null
    Remove-Variable -Name Key -ErrorAction SilentlyContinue
} else {
    Write-Host "Enter $Name (input hidden):" -ForegroundColor Cyan
    $secure = Read-Host -AsSecureString
}

if (-not $secure -or $secure.Length -eq 0) {
    Write-Error "Empty key, aborted."
    exit 1
}

# DPAPI 加密（用 Windows CurrentUser 凭据，只有当前账号在本机能解密）
$encrypted = ConvertFrom-SecureString -SecureString $secure

$existing += "$Name=$encrypted"
Set-Content -LiteralPath $secureFile -Value $existing -Encoding UTF8

# 设置文件 ACL：仅当前用户可读写（防止其他本地账号偷读取）
try {
    $acl = Get-Acl -LiteralPath $secureFile
    $acl.SetAccessRuleProtection($true, $false)  # 断开继承
    $rule = New-Object System.Security.AccessControl.FileSystemAccessRule(
        [System.Security.Principal.WindowsIdentity]::GetCurrent().Name,
        "FullControl", "Allow"
    )
    $acl.AddAccessRule($rule)
    Set-Acl -LiteralPath $secureFile -AclObject $acl
} catch {
    Write-Warning "ACL hardening failed (not critical, file still encrypted): $_"
}

Write-Host ""
Write-Host "OK. Key '$Name' encrypted and saved." -ForegroundColor Green
Write-Host "  File:        $secureFile"
Write-Host "  Encryption:  Windows DPAPI (CurrentUser scope)"
Write-Host "  Decryptable: Only by Windows user '$([System.Security.Principal.WindowsIdentity]::GetCurrent().Name)' on this machine"
Write-Host ""
Write-Host "To use: .\scripts\start-local.ps1 will load and decrypt at startup."
Write-Host "To remove: .\scripts\setup-key.ps1 -Remove"
