[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Executable,

    [Parameter(Mandatory = $true)]
    [string]$OutputDirectory,

    [Parameter(Mandatory = $true)]
    [string]$Version,

    [string]$TimestampUrl = "http://timestamp.digicert.com"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Find-SignTool {
    $roots = @(
        "${env:ProgramFiles(x86)}\Windows Kits\10\bin",
        "$env:ProgramFiles\Windows Kits\10\bin"
    ) | Where-Object { $_ -and (Test-Path $_) }

    $candidates = foreach ($root in $roots) {
        Get-ChildItem -Path $root -Filter signtool.exe -Recurse -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -match '[\\/]x64[\\/]signtool\.exe$' }
    }
    $tool = $candidates | Sort-Object FullName -Descending | Select-Object -First 1
    if (-not $tool) {
        throw "signtool.exe x64 non trovato nel Windows SDK."
    }
    return $tool.FullName
}

if (-not (Test-Path -LiteralPath $Executable -PathType Leaf)) {
    throw "Eseguibile non trovato: $Executable"
}
if ([string]::IsNullOrWhiteSpace($env:WINDOWS_CERTIFICATE_BASE64)) {
    throw "Firma obbligatoria: manca WINDOWS_CERTIFICATE_BASE64. La release non verrà creata."
}
if ([string]::IsNullOrWhiteSpace($env:WINDOWS_CERTIFICATE_PASSWORD)) {
    throw "Firma obbligatoria: manca WINDOWS_CERTIFICATE_PASSWORD. La release non verrà creata."
}

$resolvedExe = (Resolve-Path -LiteralPath $Executable).Path
$resolvedOutput = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $resolvedOutput | Out-Null
$temporaryPfx = Join-Path ([IO.Path]::GetTempPath()) ("flashfit-signing-" + [guid]::NewGuid().ToString("N") + ".pfx")

try {
    try {
        $certificateBytes = [Convert]::FromBase64String($env:WINDOWS_CERTIFICATE_BASE64)
    } catch {
        throw "WINDOWS_CERTIFICATE_BASE64 non contiene un PFX Base64 valido."
    }
    [IO.File]::WriteAllBytes($temporaryPfx, $certificateBytes)

    $flags = [Security.Cryptography.X509Certificates.X509KeyStorageFlags]::EphemeralKeySet
    $certificate = [Security.Cryptography.X509Certificates.X509Certificate2]::new(
        $temporaryPfx,
        $env:WINDOWS_CERTIFICATE_PASSWORD,
        $flags
    )
    if (-not $certificate.HasPrivateKey) {
        throw "Il certificato non contiene la chiave privata necessaria alla firma."
    }
    if ($certificate.PublicKey.Oid.Value -ne "1.2.840.113549.1.1.1") {
        throw "Smart App Control richiede un certificato RSA; il certificato fornito non è RSA."
    }
    if ($certificate.NotAfter -le (Get-Date).AddDays(7)) {
        throw "Il certificato è scaduto o scade entro sette giorni."
    }

    $signTool = Find-SignTool
    & $signTool sign /fd SHA256 /td SHA256 /tr $TimestampUrl /f $temporaryPfx /p $env:WINDOWS_CERTIFICATE_PASSWORD $resolvedExe
    if ($LASTEXITCODE -ne 0) {
        throw "SignTool non è riuscito a firmare l'eseguibile (exit $LASTEXITCODE)."
    }
    & $signTool verify /pa /all /v $resolvedExe
    if ($LASTEXITCODE -ne 0) {
        throw "La verifica Authenticode è fallita (exit $LASTEXITCODE)."
    }

    $signature = Get-AuthenticodeSignature -FilePath $resolvedExe
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid) {
        throw "Firma Authenticode non valida: $($signature.Status) — $($signature.StatusMessage)"
    }
    if (-not $signature.TimeStamperCertificate) {
        throw "Manca il timestamp RFC 3161: il pacchetto non è pubblicabile."
    }

    $hash = (Get-FileHash -LiteralPath $resolvedExe -Algorithm SHA256).Hash.ToLowerInvariant()
    $fileName = [IO.Path]::GetFileName($resolvedExe)
    $checksumsPath = Join-Path $resolvedOutput "SHA256SUMS.txt"
    [IO.File]::WriteAllText($checksumsPath, "$hash  $fileName`n", [Text.UTF8Encoding]::new($false))

    $signatureInfo = [ordered]@{
        product = "FlashFit AI"
        version = $Version
        file = $fileName
        sha256 = $hash
        signature_status = $signature.Status.ToString()
        publisher = $signature.SignerCertificate.Subject
        issuer = $signature.SignerCertificate.Issuer
        certificate_thumbprint = $signature.SignerCertificate.Thumbprint
        certificate_not_after = $signature.SignerCertificate.NotAfter.ToUniversalTime().ToString("o")
        timestamp_authority = $signature.TimeStamperCertificate.Subject
    }
    $signaturePath = Join-Path $resolvedOutput "signature.json"
    [IO.File]::WriteAllText($signaturePath, ($signatureInfo | ConvertTo-Json -Depth 4), [Text.UTF8Encoding]::new($false))

    $packagePath = Join-Path $resolvedOutput "FlashFit-AI-Windows-11-x64.zip"
    if (Test-Path -LiteralPath $packagePath) {
        Remove-Item -LiteralPath $packagePath -Force
    }
    Compress-Archive -LiteralPath @($resolvedExe, $checksumsPath, $signaturePath) -DestinationPath $packagePath -CompressionLevel Optimal
    Write-Host "Pacchetto firmato e verificato: $packagePath"
    Write-Host "SHA-256: $hash"
} finally {
    if ($null -ne (Get-Variable certificate -ErrorAction SilentlyContinue)) {
        $certificate.Dispose()
    }
    if (Test-Path -LiteralPath $temporaryPfx) {
        Remove-Item -LiteralPath $temporaryPfx -Force
    }
    if ($null -ne (Get-Variable certificateBytes -ErrorAction SilentlyContinue)) {
        [Array]::Clear($certificateBytes, 0, $certificateBytes.Length)
    }
}
