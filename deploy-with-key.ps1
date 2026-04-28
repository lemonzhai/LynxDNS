param(
    [string]$RouterIP = "172.16.1.254",
    [string]$User = "root",
    [string]$Password = "Santaovid6688",
    [string]$Arch = "x86_64"
)

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
$DeployScript = Join-Path $Root "deploy.ps1"
$Putty = "C:\Program Files\PuTTY\putty.exe"
$Plink = "C:\Program Files\PuTTY\plink.exe"

Write-Host "=== Deploy LynxDNS -> $User@$RouterIP ($Arch) ===" -ForegroundColor Cyan

# First, try to connect with putty to accept host key
Write-Host "[1/2] Accepting host key..." -ForegroundColor Yellow
try {
    # Create a temporary batch file to handle the connection
    $batchFile = Join-Path $env:TEMP "accept-host-key.bat"
    $batchContent = @"
@echo off
"$Putty" -ssh $User@$RouterIP -pw "$Password" -cmd "exit" 2>NUL
"@
    Set-Content -Path $batchFile -Value $batchContent
    
    # Run the batch file to accept host key
    Start-Process -FilePath "cmd.exe" -ArgumentList "/c $batchFile" -Wait -WindowStyle Hidden
    
    # Clean up
    Remove-Item -Path $batchFile -Force -ErrorAction SilentlyContinue
    
    Write-Host "Host key accepted successfully" -ForegroundColor Green
} catch {
    Write-Host "Warning: Failed to accept host key: $($_.Exception.Message)" -ForegroundColor Yellow
}

# Now run the original deploy script
Write-Host "[2/2] Running deploy script..." -ForegroundColor Yellow
& $DeployScript -RouterIP $RouterIP -User $User -Password $Password -Arch $Arch

Write-Host "`n=== Deploy process completed ===" -ForegroundColor Cyan