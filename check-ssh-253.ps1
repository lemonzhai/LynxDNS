$RouterIP = "172.16.1.253"
$User = "root"

Write-Host "=== Checking LynxDNS Deployment ===" -ForegroundColor Cyan
Write-Host "Router: $User@$RouterIP" -ForegroundColor Cyan

# Check if service is running
Write-Host "`n1. Checking service status:" -ForegroundColor Yellow
try {
    $result = ssh -o StrictHostKeyChecking=no $User@$RouterIP "ps w | grep lynxdns | grep -v grep | head -1"
    if ($result -match "lynxdns") {
        Write-Host "Service is running" -ForegroundColor Green
        $procPid = ($result -split '\s+')[0]
        Write-Host "PID: $procPid" -ForegroundColor Green
    } else {
        Write-Host "Service is not running" -ForegroundColor Red
    }
} catch {
    Write-Host "Cannot connect to router: $($_.Exception.Message)" -ForegroundColor Red
}

# Check if LynxDNS files exist
Write-Host "`n2. Checking LynxDNS files:" -ForegroundColor Yellow
try {
    $files = ssh -o StrictHostKeyChecking=no $User@$RouterIP "ls -la /etc/lynxdns/"
    Write-Host "Files in /etc/lynxdns/:" -ForegroundColor Cyan
    Write-Host $files
} catch {
    Write-Host "Cannot check files: $($_.Exception.Message)" -ForegroundColor Red
}

Write-Host "`n=== Check completed ===" -ForegroundColor Cyan