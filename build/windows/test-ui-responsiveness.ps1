[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Executable,

    [int]$ResizeCycles = 12
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Add-Type @'
using System;
using System.Runtime.InteropServices;

public static class FlashFitUiProbe {
    [DllImport("user32.dll", SetLastError = true)]
    public static extern IntPtr SendMessageTimeout(
        IntPtr hWnd,
        uint message,
        IntPtr wParam,
        IntPtr lParam,
        uint flags,
        uint timeout,
        out IntPtr result);

    [DllImport("user32.dll")]
    public static extern bool ShowWindow(IntPtr hWnd, int command);

    [DllImport("user32.dll")]
    public static extern bool PostMessage(IntPtr hWnd, uint message, IntPtr wParam, IntPtr lParam);

    [DllImport("user32.dll")]
    public static extern uint GetGuiResources(IntPtr process, uint flags);
}
'@

function Assert-Responsive([IntPtr]$Window) {
    $reply = [IntPtr]::Zero
    # WM_NULL + SMTO_BLOCK | SMTO_ABORTIFHUNG, one-second deadline.
    $ok = [FlashFitUiProbe]::SendMessageTimeout(
        $Window, 0, [IntPtr]::Zero, [IntPtr]::Zero, 3, 1000, [ref]$reply
    )
    if ($ok -eq [IntPtr]::Zero) {
        throw "La finestra non ha risposto entro un secondo."
    }
}

$resolvedExe = (Resolve-Path -LiteralPath $Executable).Path
$process = $null
try {
    $process = Start-Process -FilePath $resolvedExe -WorkingDirectory (Split-Path $resolvedExe) -PassThru
    $deadline = (Get-Date).AddSeconds(45)
    do {
        Start-Sleep -Milliseconds 250
        $process.Refresh()
    } while ($process.MainWindowHandle -eq [IntPtr]::Zero -and -not $process.HasExited -and (Get-Date) -lt $deadline)

    if ($process.HasExited) {
        throw "FlashFit è terminata prima della comparsa della finestra."
    }
    if ($process.MainWindowHandle -eq [IntPtr]::Zero) {
        throw "Finestra FlashFit non trovata entro 45 secondi."
    }

    $window = $process.MainWindowHandle
    Assert-Responsive $window
    $gdiBefore = [FlashFitUiProbe]::GetGuiResources($process.Handle, 0)

    for ($index = 0; $index -lt $ResizeCycles; $index++) {
        [FlashFitUiProbe]::ShowWindow($window, 3) | Out-Null # SW_MAXIMIZE
        Start-Sleep -Milliseconds 180
        Assert-Responsive $window
        [FlashFitUiProbe]::ShowWindow($window, 9) | Out-Null # SW_RESTORE
        Start-Sleep -Milliseconds 180
        Assert-Responsive $window
    }

    Start-Sleep -Seconds 8
    Assert-Responsive $window
    $process.Refresh()
    $gdiAfter = [FlashFitUiProbe]::GetGuiResources($process.Handle, 0)
    if ($gdiAfter -gt ($gdiBefore + 80)) {
        throw "Possibile perdita GDI dopo i resize: prima=$gdiBefore dopo=$gdiAfter."
    }
    Write-Host "UI responsive; GDI prima=$gdiBefore dopo=$gdiAfter."

    [FlashFitUiProbe]::PostMessage($window, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null # WM_CLOSE
    if (-not $process.WaitForExit(8000)) {
        throw "FlashFit non si è chiusa in modo ordinato entro otto secondi."
    }
} finally {
    if ($process -and -not $process.HasExited) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
    }
}
