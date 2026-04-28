param(
    [string]$Version = "1.0.2"
)

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
$GoExe = "C:\Program Files\Go\bin\go.exe"
$CoreDir = Join-Path $Root "lynxdns-core"
$DistBin = Join-Path $Root "dist\Openwrt\lynxdns-core\bin"

$env:GOROOT = "C:\Program Files\Go"
$env:PATH = "$env:GOROOT\bin;$env:PATH"

$BuildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$LdFlags = "-s -w -X main.version=$Version -X main.buildTime=$BuildTime"

if (-not (Test-Path $DistBin)) { New-Item -ItemType Directory -Force -Path $DistBin | Out-Null }

$targets = @(
    @{GOOS="linux"; GOARCH="amd64";  Name="x86_64"},
    @{GOOS="linux"; GOARCH="arm64";  Name="aarch64"},
    @{GOOS="linux"; GOARCH="arm";    Name="arm";     GOARM="5"},
    @{GOOS="linux"; GOARCH="arm";    Name="armv7";   GOARM="7"},
    @{GOOS="linux"; GOARCH="mipsle"; Name="mipsel-softfloat"},
    @{GOOS="linux"; GOARCH="mipsle"; Name="mipsel-hardfloat"},
    @{GOOS="linux"; GOARCH="mips";   Name="mips-softfloat"},
    @{GOOS="linux"; GOARCH="mips";   Name="mips-hardfloat"}
)

Push-Location $CoreDir
$ok = 0; $fail = 0

foreach ($t in $targets) {
    $env:GOOS = $t.GOOS
    $env:GOARCH = $t.GOARCH
    $env:GOARM = if ($t.GOARM) { $t.GOARM } else { $null }
    $env:CGO_ENABLED = "0"
    $out = Join-Path $DistBin "lynxdns-$($t.Name)"

    & $GoExe build -ldflags $LdFlags -trimpath -o $out "./cmd/lynxdns" 2>&1 | Out-Null
    if ($LASTEXITCODE -eq 0) {
        Write-Host "  [OK] $($t.Name)" -ForegroundColor Green
        $ok++
    } else {
        Write-Host "  [FAIL] $($t.Name)" -ForegroundColor Red
        $fail++
    }
}

$env:GOOS = $null; $env:GOARCH = $null; $env:GOARM = $null; $env:CGO_ENABLED = $null
Pop-Location

$distCore = Join-Path $Root "dist\Openwrt\lynxdns-core"
Copy-Item (Join-Path $CoreDir "configs\config.yaml") $distCore -Force
$dataDir = Join-Path $distCore "data"
if (-not (Test-Path $dataDir)) { New-Item -ItemType Directory -Force -Path $dataDir | Out-Null }
Copy-Item (Join-Path $CoreDir "data\*") $dataDir -Force -Recurse

$distLuci = Join-Path $Root "dist\Openwrt\luci-app-lynxdns"
if (Test-Path $distLuci) { Remove-Item $distLuci -Recurse -Force }
Copy-Item (Join-Path $Root "luci-app-lynxdns") $distLuci -Recurse -Force

Write-Host ""
Write-Host "Build done: $ok OK, $fail FAIL  |  Version: $Version" -ForegroundColor $(if ($fail -eq 0){"Green"}else{"Yellow"})
