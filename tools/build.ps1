# Builds FlashFit AI from source.
#
# A binary you compile yourself never carries the Mark-of-the-Web, so Windows
# runs it with no SmartScreen prompt — not because anything was disabled, but
# because the file was never downloaded. For an open-source app this is the most
# honest install there is: you can read every line first, then run what you
# built.
#
# Needs Go (https://go.dev/dl/). Nothing else.
#
#   .\tools\build.ps1                 # builds to .\dist
#   .\tools\build.ps1 -Desktop        # also copies to the Desktop
#   .\tools\build.ps1 -Sign           # signs it too (see tools\sign.ps1)

[CmdletBinding()]
param(
    [string]$OutputDir = "dist",
    [switch]$Desktop,
    [switch]$Sign
)

$ErrorActionPreference = "Stop"
Set-Location (Split-Path -Parent $PSScriptRoot)

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go non trovato. Installalo da https://go.dev/dl/ e riprova."
}

$version = (Select-String -Path "appnative\main_windows.go" -Pattern 'buildVersion = "([^"]+)"').Matches[0].Groups[1].Value
Write-Host "Compilo FlashFit AI $version" -ForegroundColor Cyan

Write-Host "Eseguo i test..." -ForegroundColor Cyan
go test ./...
if ($LASTEXITCODE -ne 0) { throw "I test non passano: build interrotta." }

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
$exe = Join-Path $OutputDir "FlashFit-AI-Windows-11.exe"
go build -trimpath -ldflags "-s -w -H windowsgui" -o $exe ./appnative
if ($LASTEXITCODE -ne 0) { throw "Compilazione fallita." }

Write-Host "Compilato: $exe" -ForegroundColor Green

if ($Sign) {
    & "$PSScriptRoot\sign.ps1" -Path $exe
}

if ($Desktop) {
    $target = Join-Path ([Environment]::GetFolderPath("Desktop")) "FlashFit AI.exe"
    Copy-Item $exe $target -Force
    Write-Host "Copiato sul Desktop: $target" -ForegroundColor Green
}

Write-Host ""
Write-Host "Nessun marchio 'scaricato da Internet': Windows lo avvia senza avvisi."
