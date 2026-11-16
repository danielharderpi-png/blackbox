# Installs `blackbox` and `bbox` on Windows (also works with PowerShell on Linux/macOS).
# Requires Go 1.21+. Adds the Go bin directory to your *User* PATH if missing.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go is not installed (https://go.dev/dl/)'
}

go install ./cmd/blackbox ./cmd/bbox
if ($LASTEXITCODE -ne 0) { throw 'go install failed' }

$bin = (go env GOBIN).Trim()
if (-not $bin) { $bin = Join-Path (go env GOPATH).Trim() 'bin' }
Write-Host "installed blackbox and bbox to $bin"

$sep = [IO.Path]::PathSeparator
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$entries = @($userPath -split [regex]::Escape($sep) | Where-Object { $_ })
$onPath = $entries | Where-Object { $_.TrimEnd('\', '/') -ieq $bin.TrimEnd('\', '/') }

if (-not $onPath -and $IsWindows -ne $false) {
    $new = (@($entries) + $bin) -join $sep
    [Environment]::SetEnvironmentVariable('Path', $new, 'User')
    Write-Host "added $bin to your User PATH - open a new terminal to pick it up"
} elseif (-not $onPath) {
    Write-Host "note: add $bin to your PATH"
}
if (($env:Path -split [regex]::Escape($sep)) -notcontains $bin) { $env:Path += "$sep$bin" }
