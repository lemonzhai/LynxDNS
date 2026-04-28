$RouterIP = "172.16.1.254"
$User = "root"
$Password = "Santaovid6688"

Write-Host "=== Checking LynxDNS Deployment ===" -ForegroundColor Cyan
Write-Host "Router: $User@$RouterIP" -ForegroundColor Cyan

# Try to connect using PowerShell SSH
Write-Host "`n1. Checking service status:" -ForegroundColor Yellow
try {
    # Use PowerShell SSH with -HostKey parameter to avoid fingerprint prompt
    $sshCommand = "ssh -o StrictHostKeyChecking=no $User@$RouterIP 'ps w | grep lynxdns | grep -v grep | head -1'"
    $serviceStatus = Invoke-Expression $sshCommand 2>&1
    
    if ($serviceStatus -match "lynxdns") {
        Write-Host "Service is running" -ForegroundColor Green
        $procPid = ($serviceStatus -split '\s+')[0]
        Write-Host "PID: $procPid" -ForegroundColor Green
    } else {
        Write-Host "Service is not running" -ForegroundColor Red
    }
} catch {
    Write-Host "Cannot connect to router: $($_.Exception.Message)" -ForegroundColor Red
}

Write-Host "`n=== Check completed ===" -ForegroundColor Cyan