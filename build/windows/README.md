# Windows trusted release gate

The public Windows package must never be produced from an unsigned executable.
Run the `Build signed Windows candidate` workflow with two encrypted repository
secrets:

- `WINDOWS_CERTIFICATE_BASE64`: Base64 of a valid RSA Authenticode PFX issued by
  a certification authority in the Microsoft Trusted Root Program.
- `WINDOWS_CERTIFICATE_PASSWORD`: password of that PFX.

The workflow tests and builds the app, repeatedly maximizes/restores the real
window while probing its one-second response deadline and GDI-object growth,
then signs it with SHA-256, obtains an RFC 3161 timestamp, verifies the
Authenticode chain, and generates both `SHA256SUMS.txt` and `signature.json`.
It uploads a short-lived candidate artifact; it does not publish a GitHub
release.

Before compilation it also embeds the product icon, Windows 11 DPI-aware
manifest, and Explorer version metadata using the pinned `go-winres` v0.3.3
tool. Those resources improve identification and presentation but do not replace
the trusted Authenticode signature.

If the certificate is missing, expired, non-RSA, has no private key, or the
signature/timestamp cannot be verified, the job fails and creates no package.
Never replace this gate with a self-signed certificate: Windows does not treat
one as a trusted publisher.
