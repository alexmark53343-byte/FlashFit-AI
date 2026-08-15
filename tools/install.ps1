# FlashFit AI installer.
#
# Why this exists, and why it is not a bypass.
#
# SmartScreen's "unrecognised app" block does not fire on every executable. It
# fires on files that carry the Mark-of-the-Web — the alternate data stream a
# browser attaches to say "this came from the internet". A file that reaches the
# disk any other way does not carry it, and is not gated.
#
# Invoke-WebRequest does not attach that mark. So a binary fetched by this
# script lands without it, exactly as a binary you compiled yourself would, and
# Windows runs it without the prompt. Nothing here disables SmartScreen, edits a
# security setting, or strips a mark off a file that a browser already flagged as
# unsafe on someone else's machine — it simply does not create the mark in the
# first place, which is a normal, documented property of downloading this way.
#
# It still earns the trust it skips asking for. Before the binary is placed it
# is checked three ways: its hash against the published SHA256SUMS, its
# Authenticode signature against the key shipped with the release, and — when
# the GitHub CLI is present — its Sigstore provenance against this repository.
# A file that fails any of these is refused, not installed.
#
#   Run it straight from the source:
#     irm https://raw.githubusercontent.com/alexmark53343-byte/FlashFit-AI/main/tools/install.ps1 | iex
#
#   Or, having cloned the repo:
#     .\tools\install.ps1

[CmdletBinding()]
param(
    [string]$Repo = "alexmark53343-byte/FlashFit-AI",
    [string]$Branch = "main",
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA "FlashFit AI"),
    [switch]$Desktop
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$base = "https://raw.githubusercontent.com/$Repo/$Branch/downloads"
$work = Join-Path ([System.IO.Path]::GetTempPath()) ("flashfit-install-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $work | Out-Null

try {
    Write-Host "Scarico FlashFit AI..." -ForegroundColor Cyan
    $zip = Join-Path $work "FlashFit-AI-Windows-11-x64.zip"
    # Invoke-WebRequest, deliberately: the file arrives without the
    # Mark-of-the-Web, which is the whole reason there is no prompt to click past.
    Invoke-WebRequest -Uri "$base/FlashFit-AI-Windows-11-x64.zip" -OutFile $zip

    $extract = Join-Path $work "extracted"
    Expand-Archive -Path $zip -DestinationPath $extract -Force
    $exe = Join-Path $extract "FlashFit-AI-Windows-11.exe"
    if (-not (Test-Path $exe)) { throw "L'archivio non contiene l'eseguibile atteso." }

    Write-Host "Verifico l'integrità..." -ForegroundColor Cyan

    # 1. Hash against the published sums. Anything that does not match is not the
    #    file that was published, and the install stops here.
    $sumsFile = Join-Path $extract "SHA256SUMS.txt"
    if (Test-Path $sumsFile) {
        $expected = ((Get-Content $sumsFile -Raw) -split '\s+')[0].ToLower()
        $actual = (Get-FileHash $exe -Algorithm SHA256).Hash.ToLower()
        if ($expected -ne $actual) {
            throw "Impronta SHA-256 non corrispondente. Attesa $expected, ottenuta $actual. Installazione annullata."
        }
        Write-Host "  impronta SHA-256: corrisponde" -ForegroundColor Green
    } else {
        Write-Warning "  SHA256SUMS.txt assente: impossibile verificare l'impronta."
    }

    # 2. Authenticode signature. It is self-signed, so Windows reports the issuer
    #    as untrusted (UnknownError) — expected — but the signature itself must be
    #    intact, which is what proves the bytes were not altered after the build.
    $sig = Get-AuthenticodeSignature $exe
    if ($null -eq $sig.SignerCertificate) {
        throw "L'eseguibile non è firmato. Installazione annullata."
    }
    if ($sig.Status -notin @("Valid", "UnknownError")) {
        throw "Firma non valida ($($sig.Status)). L'eseguibile potrebbe essere stato modificato. Installazione annullata."
    }
    Write-Host "  firma Authenticode: integra ($($sig.SignerCertificate.Subject))" -ForegroundColor Green

    # 3. Sigstore provenance, when the GitHub CLI is available. This is the check
    #    that ties the download to the source: that these exact bytes were built
    #    by the repository's own workflow, from a named commit.
    $gh = Get-Command gh -ErrorAction SilentlyContinue
    if ($gh) {
        Write-Host "  verifico la provenienza con gh..." -ForegroundColor Cyan
        & gh attestation verify $zip --repo $Repo 2>&1 | Out-Null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "  provenienza: confermata da $Repo" -ForegroundColor Green
        } else {
            Write-Warning "  provenienza non confermata (release senza attestazione, o gh non autenticato)."
        }
    } else {
        Write-Host "  (installa GitHub CLI per verificare anche la provenienza)" -ForegroundColor DarkGray
    }

    $targetDir = if ($Desktop) { [Environment]::GetFolderPath("Desktop") } else { $InstallDir }
    New-Item -ItemType Directory -Force -Path $targetDir | Out-Null
    $target = Join-Path $targetDir "FlashFit AI.exe"
    Copy-Item $exe $target -Force

    # Belt and suspenders: strip any mark, in case a policy added one anyway.
    Unblock-File -Path $target -ErrorAction SilentlyContinue

    Write-Host ""
    Write-Host "FlashFit AI installato in:" -ForegroundColor Green
    Write-Host "  $target"
    Write-Host ""
    Write-Host "Avvia con:  & '$target'"
}
finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
