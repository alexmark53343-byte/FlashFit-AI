# Signs a FlashFit AI release binary.
#
# What this gives you, and what it does not.
#
# The certificate is self-signed, so Windows can verify the signature but does
# not trust who made it: SmartScreen still reports an unknown publisher, exactly
# as it did before. Only a certificate issued by a recognised authority changes
# that, and those cost money and require identity verification.
#
# What it does give is worth having in the meantime. The binary carries a
# cryptographic signature over its own bytes, so any modification after the
# build breaks it and says so. The signature is timestamped, so it stays valid
# after the certificate expires. And the public half of the key ships alongside
# the download, so anyone can check that the executable they have came from the
# same key as every other release rather than from someone else.
#
# The private key lives in the current user's personal certificate store and is
# never in this repository. Back it up: losing it means future releases are
# signed by a different key, which is indistinguishable from someone else
# signing them.
#
#   .\tools\sign.ps1 -Path .\dist\FlashFit-AI-Windows-11.exe

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Path,
    [string]$Subject = "CN=FlashFit AI, O=FlashFit AI, C=IT",
    [string]$TimestampServer = "http://timestamp.digicert.com",
    [string]$ExportPublicKeyTo = "signing\FlashFit-AI-CodeSigning.cer"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $Path)) { throw "binario non trovato: $Path" }

$cert = Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert |
    Where-Object { $_.Subject -eq $Subject -and $_.NotAfter -gt (Get-Date) } |
    Sort-Object NotAfter -Descending |
    Select-Object -First 1

if ($null -eq $cert) {
    Write-Host "Nessun certificato valido per ${Subject}: ne creo uno nuovo."
    # CurrentUser\My is the user's own key store. Nothing here is added to any
    # trust root — establishing trust in a self-signed key is a decision for
    # whoever installs it, not for the build script.
    $cert = New-SelfSignedCertificate `
        -Type CodeSigningCert `
        -Subject $Subject `
        -KeyUsage DigitalSignature `
        -FriendlyName "FlashFit AI code signing (self-signed)" `
        -CertStoreLocation "Cert:\CurrentUser\My" `
        -NotAfter (Get-Date).AddYears(5) `
        -KeyAlgorithm RSA -KeyLength 3072 -HashAlgorithm SHA256
}

Write-Host "Firmo con $($cert.Subject)"
Write-Host "Impronta del certificato: $($cert.Thumbprint)"

$result = Set-AuthenticodeSignature -FilePath $Path -Certificate $cert `
    -HashAlgorithm SHA256 -TimestampServer $TimestampServer

$signature = Get-AuthenticodeSignature $Path
if ($null -eq $signature.SignerCertificate) {
    throw "la firma non e' stata applicata: $($result.StatusMessage)"
}
if ($null -eq $signature.TimeStamperCertificate) {
    # Without a timestamp the signature dies with the certificate, which defeats
    # most of the point of applying one.
    throw "firma applicata ma senza marca temporale: server non raggiungibile?"
}

# UnknownError is the expected status for a self-signed certificate: the chain
# is intact and ends at a root this machine has no reason to trust. Anything
# else means the signature itself is wrong.
if ($signature.Status -notin @("Valid", "UnknownError")) {
    throw "stato della firma inatteso: $($signature.Status) - $($signature.StatusMessage)"
}

if ($ExportPublicKeyTo) {
    $dir = Split-Path -Parent $ExportPublicKeyTo
    if ($dir -and -not (Test-Path $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    Export-Certificate -Cert $cert -FilePath $ExportPublicKeyTo -Type CERT | Out-Null
    Write-Host "Chiave pubblica esportata in $ExportPublicKeyTo"
}

Write-Host "Firmato : $Path"
Write-Host "Algoritmo: $($signature.SignerCertificate.SignatureAlgorithm.FriendlyName)"
Write-Host "Stato    : $($signature.Status) (auto-firmato: SmartScreen mostrera' comunque editore sconosciuto)"
