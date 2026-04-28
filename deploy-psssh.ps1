param(
    [string]$RouterIP = "172.16.1.254",
    [string]$User = "root",
    [string]$Password = "Santaovid6688",
    [string]$Arch = "x86_64"
)

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
$Dist = Join-Path $Root "dist\Openwrt"
$Core = Join-Path $Dist "lynxdns-core"
$Luci = Join-Path $Dist "luci-app-lynxdns"

function Remote-Exec($cmd) {
    # Use PowerShell SSH with password authentication
    $command = "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ${User}@${RouterIP} '$cmd'"
    Write-Host "Executing: $command" -ForegroundColor Gray
    Invoke-Expression $command 2>&1
}

function Remote-Copy($src, $dst) {
    # Use PowerShell SCP with password authentication
    $command = "scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null $src ${User}@${RouterIP}:$dst"
    Write-Host "Copying: $src -> $dst" -ForegroundColor Gray
    Invoke-Expression $command 2>&1
}

function Remote-CopyWild($src, $dst) {
    # Use PowerShell SCP with password authentication for wildcard files
    $command = "scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null $src ${User}@${RouterIP}:$dst"
    Write-Host "Copying: $src -> $dst" -ForegroundColor Gray
    Invoke-Expression $command 2>&1
}

Write-Host "=== Deploy LynxDNS -> $User@$RouterIP ($Arch) ===" -ForegroundColor Cyan

Write-Host "[1/8] Stopping service..." -ForegroundColor Yellow
Remote-Exec "/etc/init.d/lynxdns stop 2>/dev/null || true" | Out-Null

Write-Host "[2/8] Creating directories..." -ForegroundColor Yellow
Remote-Exec "mkdir -p /etc/lynxdns /usr/share/luci/luci-static/resources/view/lynxdns /usr/share/rpcd/acl.d /usr/share/luci/menu.d /etc/uci-defaults" | Out-Null

Write-Host "[3/8] Uploading binary..." -ForegroundColor Yellow
Remote-Copy "$Core\bin\lynxdns-$Arch" "/etc/lynxdns/lynxdns"
Remote-Exec "chmod +x /etc/lynxdns/lynxdns" | Out-Null

Write-Host "[4/8] Uploading config..." -ForegroundColor Yellow
Remote-Copy "$Core\config.yaml" "/etc/lynxdns/config.yaml"

Write-Host "[5/8] Syncing UCI config..." -ForegroundColor Yellow
Remote-Exec "cat > /etc/config/lynxdns << 'UCI'
config lynxdns 'main'
	option enabled '1'
UCI" | Out-Null

Write-Host "[6/8] Ensuring data directory (skip if exists)..." -ForegroundColor Yellow
Remote-Exec "mkdir -p /etc/lynxdns/data" | Out-Null

Write-Host "[7/9] Uploading LuCI files..." -ForegroundColor Yellow
Remote-Exec "mkdir -p /usr/share/luci/luci-static/resources/view/lynxdns /www/luci-static/resources/view/lynxdns" | Out-Null
Remote-CopyWild "$Luci\htdocs\luci-static\resources\view\lynxdns\*.js" "/usr/share/luci/luci-static/resources/view/lynxdns/"
Remote-CopyWild "$Luci\htdocs\luci-static\lynxdns.css" "/usr/share/luci/luci-static/"
Remote-Exec "ln -sf /usr/share/luci/luci-static/resources/view/lynxdns /www/luci-static/resources/view/lynxdns" | Out-Null
Remote-Exec "ln -sf /usr/share/luci/luci-static/lynxdns.css /www/luci-static/lynxdns.css" | Out-Null
Remote-Copy "$Luci\luasrc\controller\lynxdns.lua" "/usr/lib/lua/luci/controller/lynxdns.lua"
Remote-Copy "$Luci\root\etc\init.d\lynxdns" "/etc/init.d/lynxdns"
Remote-Exec "chmod +x /etc/init.d/lynxdns" | Out-Null
Remote-Copy "$Luci\root\usr\share\luci\menu.d\luci-app-lynxdns.json" "/usr/share/luci/menu.d/"
Remote-Copy "$Luci\root\usr\share\rpcd\acl.d\luci-app-lynxdns.json" "/usr/share/rpcd/acl.d/"
Remote-Copy "$Luci\root\etc\uci-defaults\90-luci-lynxdns" "/etc/uci-defaults/"

Write-Host "[8/9] Clearing LuCI cache..." -ForegroundColor Yellow
Remote-Exec "rm -f /tmp/luci-indexcache* /tmp/luci-modulecache/* 2>/dev/null" | Out-Null
Remote-Exec "/etc/init.d/rpcd restart 2>/dev/null || true" | Out-Null
Remote-Exec "/etc/init.d/uhttpd restart 2>/dev/null || /etc/init.d/nginx restart 2>/dev/null" | Out-Null

Write-Host "[9/9] Starting service..." -ForegroundColor Yellow
Remote-Exec "/etc/init.d/lynxdns enable 2>/dev/null || true" | Out-Null
Remote-Exec "/etc/init.d/lynxdns start" | Out-Null

$result = Remote-Exec "ps w | grep lynxdns | grep -v grep | head -1"
if ($result -match "lynxdns") {
    $version = Remote-Exec "cat /etc/lynxdns/version.txt 2>/dev/null || echo 'unknown'"
    $procPid = ($result -split '\s+')[0]
    Write-Host "VERSION=LynxDNS v1.0.2" -ForegroundColor Green
    Write-Host "PID=$procPid" -ForegroundColor Green
} else {
    Write-Host "WARNING: Service may not have started correctly" -ForegroundColor Red
}

Write-Host ""
Write-Host "=== Deploy complete ===" -ForegroundColor Cyan