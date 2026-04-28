$RouterIP = "172.16.1.254"
$User = "root"
$Password = "Santaovid6688"
$Arch = "x86_64"

Write-Host "=== 部署 LynxDNS 到 $User@$RouterIP ===" -ForegroundColor Cyan

# 调用现有的部署脚本
& .\deploy.ps1 -RouterIP $RouterIP -User $User -Password $Password -Arch $Arch

Write-Host "`n=== 部署完成 ===" -ForegroundColor Cyan