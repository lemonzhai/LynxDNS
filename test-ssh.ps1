param(
    [string]$RouterIP = "172.16.1.254",
    [string]$User = "root",
    [string]$Password = "Santaovid6688"
)

$ErrorActionPreference = "Continue"

Write-Host "=== Testing SSH Connection ===" -ForegroundColor Cyan
Write-Host "Router: $User@$RouterIP" -ForegroundColor Cyan

# Test basic SSH connection
try {
    Write-Host "`n1. Testing basic connection..." -ForegroundColor Yellow
    $result = ssh -o StrictHostKeyChecking=no $User@$RouterIP "echo 'SSH connection test successful'"
    Write-Host "Result: $result" -ForegroundColor Green
} catch {
    Write-Host "Error: $($_.Exception.Message)" -ForegroundColor Red
}

# Test if LynxDNS directory exists
try {
    Write-Host "`n2. Checking if LynxDNS directory exists..." -ForegroundColor Yellow
    $result = ssh -o StrictHostKeyChecking=no $User@$RouterIP "ls -la /etc/lynxdns 2>/dev/null || echo 'Directory not found'"
    Write-Host "Result: $result" -ForegroundColor Cyan
} catch {
    Write-Host "Error: $($_.Exception.Message)" -ForegroundColor Red
}

# Test service status
try {
    Write-Host "`n3. Checking LynxDNS service status..." -ForegroundColor Yellow
    $result = ssh -o StrictHostKeyChecking=no $User@$RouterIP "/etc/init.d/lynxdns status 2>/dev/null || echo 'Service not found'"
    Write-Host "Result: $result" -ForegroundColor Cyan
} catch {
    Write-Host "Error: $($_.Exception.Message)" -ForegroundColor Red
}

Write-Host "`n=== Test completed ===" -ForegroundColor Cyan