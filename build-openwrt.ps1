param(
    [string]$Version = "1.0.0",
    [string]$OutputDir = "dist\Openwrt"
)

$ErrorActionPreference = "Stop"
$ProjectRoot = $PSScriptRoot
$CoreDir = Join-Path $ProjectRoot "lynxdns-core"
$LuciDir = Join-Path $ProjectRoot "luci-app-lynxdns"
$DistDir = Join-Path $ProjectRoot $OutputDir

$BuildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$GoVersion = (go version 2>$null).ToString().Split()[2] -replace "go", ""

$Architectures = @(
    @{GOOS="linux"; GOARCH="mipsle"; Name="mipsel-softfloat"; CC="mipsel-linux-gnu-gcc"; CFLAGS="-msoft-float"},
    @{GOOS="linux"; GOARCH="mipsle"; Name="mipsel-hardfloat"; CC="mipsel-linux-gnu-gcc"; CFLAGS=""},
    @{GOOS="linux"; GOARCH="mips"; Name="mips-softfloat"; CC="mips-linux-gnu-gcc"; CFLAGS="-msoft-float"},
    @{GOOS="linux"; GOARCH="mips"; Name="mips-hardfloat"; CC="mips-linux-gnu-gcc"; CFLAGS=""},
    @{GOOS="linux"; GOARCH="arm"; Name="arm"; CC="arm-linux-gnueabi-gcc"; GOARM="5"},
    @{GOOS="linux"; GOARCH="arm"; Name="armv7"; CC="arm-linux-gnueabihf-gcc"; GOARM="7"},
    @{GOOS="linux"; GOARCH="arm64"; Name="aarch64"; CC="aarch64-linux-gnu-gcc"},
    @{GOOS="linux"; GOARCH="amd64"; Name="x86_64"; CC="x86_64-linux-gnu-gcc"}
)

function Write-Header {
    param([string]$Text)
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Cyan
    Write-Host " $Text" -ForegroundColor Cyan
    Write-Host "========================================" -ForegroundColor Cyan
    Write-Host ""
}

function Build-Core {
    Write-Header "Building lynxdns-core for OpenWrt"
    
    $BinDir = Join-Path $DistDir "lynxdns-core\bin"
    if (-not (Test-Path $BinDir)) {
        New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    }
    
    Push-Location $CoreDir
    
    $SuccessCount = 0
    $FailCount = 0
    
    foreach ($Arch in $Architectures) {
        $ArchName = $Arch.Name
        $OutputFile = Join-Path $BinDir "lynxdns-$ArchName"
        
        Write-Host "Building for $ArchName..." -ForegroundColor Yellow
        
        $Env:GOOS = $Arch.GOOS
        $Env:GOARCH = $Arch.GOARCH
        $Env:CGO_ENABLED = "0"
        
        if ($Arch.GOARM) {
            $Env:GOARM = $Arch.GOARM
        } else {
            $Env:GOARM = $null
        }
        
        $BuildArgs = @(
            "build",
            "-ldflags", "-s -w -X main.version=$Version -X main.buildTime=$BuildTime -X main.goVersion=$GoVersion -X main.buildOS=linux -X main.buildArch=$($Arch.GOARCH)",
            "-trimpath",
            "-o", $OutputFile,
            "./cmd/lynxdns"
        )
        
        try {
            $Result = & go $BuildArgs 2>&1
            if ($LASTEXITCODE -eq 0) {
                Write-Host "  [OK] lynxdns-$ArchName" -ForegroundColor Green
                $SuccessCount++
            } else {
                Write-Host "  [FAIL] lynxdns-$ArchName : $Result" -ForegroundColor Red
                $FailCount++
            }
        } catch {
            Write-Host "  [FAIL] lynxdns-$ArchName : $_" -ForegroundColor Red
            $FailCount++
        }
    }
    
    $Env:GOOS = $null
    $Env:GOARCH = $null
    $Env:GOARM = $null
    $Env:CGO_ENABLED = $null
    
    Pop-Location
    
    Write-Host ""
    Write-Host "Build Summary: $SuccessCount succeeded, $FailCount failed" -ForegroundColor $(if ($FailCount -eq 0) { "Green" } else { "Yellow" })
    
    Copy-ConfigFiles
}

function Copy-ConfigFiles {
    Write-Host "Copying configuration files..." -ForegroundColor Yellow
    
    $DestDir = Join-Path $DistDir "lynxdns-core"
    
    Copy-Item -Path (Join-Path $CoreDir "configs\config.yaml") -Destination $DestDir -Force
    Copy-Item -Path (Join-Path $CoreDir "data") -Destination $DestDir -Force -Recurse -ErrorAction SilentlyContinue
    
    $DataDir = Join-Path $DestDir "data"
    if (-not (Test-Path $DataDir)) {
        New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
    }
    
    Write-Host "  [OK] Configuration files copied" -ForegroundColor Green
}

function Build-Luci {
    Write-Header "Building luci-app-lynxdns"
    
    $DestDir = Join-Path $DistDir "luci-app-lynxdns"
    
    if (Test-Path $DestDir) {
        Remove-Item -Path $DestDir -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $DestDir | Out-Null
    
    Copy-Item -Path $LuciDir -Destination $DestDir -Recurse -Force
    
    Write-Host "Luci app files copied to $DestDir" -ForegroundColor Green
    
    Create-IpkPackage
}

function Create-IpkPackage {
    Write-Host "Creating IPK package structure..." -ForegroundColor Yellow
    
    $PkgDir = Join-Path $DistDir "packages"
    if (-not (Test-Path $PkgDir)) {
        New-Item -ItemType Directory -Force -Path $PkgDir | Out-Null
    }
    
    $ControlDir = Join-Path $PkgDir "luci-app-lynxdns\CONTROL"
    New-Item -ItemType Directory -Force -Path $ControlDir | Out-Null
    
    $ControlContent = @"
Package: luci-app-lynxdns
Version: $Version
Depends: libc, lynxdns, luci-base, luci-lib-base, luci-lib-jsonc, curl
Source: https://github.com/lynxdns/luci-app-lynxdns
Section: luci
Architecture: all
Installed-Size: 1024
Description: LynxDNS - Intelligent DNS Resolver Web UI for OpenWrt
 A LuCI web interface for LynxDNS DNS resolver with features including:
 - DNS routing rules management
 - GeoIP/GeoSite based routing
 - Ad filtering
 - Real-time logs and statistics
"@
    
    Set-Content -Path (Join-Path $ControlDir "control") -Value $ControlContent -Encoding ASCII
    
    $PostInstContent = @"
#!/bin/sh
[ -n "${IPKG_NO_SCRIPT}" ] && exit 0
[ -x /etc/uci-defaults/90-luci-lynxdns ] && /etc/uci-defaults/90-luci-lynxdns
exit 0
"@
    
    Set-Content -Path (Join-Path $ControlDir "postinst") -Value $PostInstContent -Encoding ASCII
    
    Write-Host "  [OK] IPK package structure created" -ForegroundColor Green
}

function Create-PackageInfo {
    $InfoFile = Join-Path $DistDir "README.txt"
    
    $Content = @"
LynxDNS OpenWrt Build Package
=============================
Version: $Version
Build Time: $BuildTime
Go Version: $GoVersion

Directory Structure:
--------------------
dist/Openwrt/
  |-- lynxdns-core/
  |   |-- bin/
  |   |   |-- lynxdns-mipsel-softfloat    (MIPS little-endian soft-float)
  |   |   |-- lynxdns-mipsel-hardfloat    (MIPS little-endian hard-float)
  |   |   |-- lynxdns-mips-softfloat      (MIPS big-endian soft-float)
  |   |   |-- lynxdns-mips-hardfloat      (MIPS big-endian hard-float)
  |   |   |-- lynxdns-arm                 (ARM v5)
  |   |   |-- lynxdns-armv7               (ARM v7 with hardware float)
  |   |   |-- lynxdns-aarch64             (ARM64/aarch64)
  |   |   |-- lynxdns-x86_64              (x86_64)
  |   |-- config.yaml                     (Default configuration)
  |   |-- data/                           (GeoIP/GeoSite data files)
  |
  |-- luci-app-lynxdns/
  |   |-- htdocs/luci-static/             (Web UI files)
  |   |-- luasrc/controller/              (LuCI controller)
  |   |-- root/etc/config/                (UCI configuration)
  |   |-- root/etc/init.d/                (Init script)
  |   |-- root/etc/uci-defaults/          (First boot scripts)
  |   |-- root/usr/share/luci/menu.d/     (Menu configuration)
  |   |-- root/usr/share/rpcd/acl.d/      (ACL configuration)
  |   |-- Makefile                        (OpenWrt package Makefile)
  |
  |-- packages/
      |-- luci-app-lynxdns/CONTROL/       (IPK package control files)

Installation:
-------------
1. Copy the appropriate lynxdns binary to /usr/bin/lynxdns on your router
2. Copy config.yaml to /etc/lynxdns/config.yaml
3. Copy luci-app-lynxdns files to corresponding directories
4. Run: /etc/init.d/lynxdns enable
5. Run: /etc/init.d/lynxdns start

Supported Devices:
------------------
- mipsel-softfloat: Xiaomi Router 3G, Newifi D2, K2P, etc.
- mipsel-hardfloat: Devices with FPU support
- mips-softfloat: Older Broadcom/Atheros devices
- aarch64: NanoPi R4S, R2S, Raspberry Pi 4, x86 soft routers
- x86_64: x86 soft routers, VMs

"@
    
    Set-Content -Path $InfoFile -Value $Content -Encoding UTF8
    Write-Host "Package info created at $InfoFile" -ForegroundColor Green
}

function Main {
    Write-Header "LynxDNS OpenWrt Build Script"
    Write-Host "Version: $Version"
    Write-Host "Output: $DistDir"
    Write-Host ""
    
    if (-not (Test-Path $DistDir)) {
        New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
    }
    
    Build-Core
    Build-Luci
    Create-PackageInfo
    
    Write-Header "Build Complete!"
    Write-Host "Output directory: $DistDir" -ForegroundColor Green
}

Main
