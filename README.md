# FlashFit AI

FlashFit AI reads a 3D model, works out how it should be printed on **your**
machine with **your** filament, and hands the slicer a project that is already
set up. It stops one step before slicing: the slicer still does the slicing.

It is an independent project by a single Italian developer. It began with a
simple frustration — advanced slicers are powerful, but finding the right
combination of settings for each model and material is still needlessly hard.

## Downloads

### Windows 11 x64 — 4.4.6 · current

[**FlashFit-AI-Windows-11-x64.zip**](https://github.com/alexmark53343-byte/FlashFit-AI/raw/main/downloads/FlashFit-AI-Windows-11-x64.zip) · 3.4 MB

Extract and run. Nothing to install. The AI model is not in the download — the
application fetches it the first time you choose one, so the app itself stays
small.

The executable is Authenticode-signed with a self-signed certificate, shipped
in the archive as `FlashFit-AI-CodeSigning.cer`. Windows verifies the signature
but has no reason to trust who issued it, so SmartScreen still reports an
unknown publisher — only a certificate from a recognised authority changes
that. What the signature does give you is tamper evidence and a stable identity
across releases: check it with `Get-AuthenticodeSignature`, and compare the
file against `SHA256SUMS.txt`. The signing step is `tools/sign.ps1`.

### macOS Apple Silicon — 3.6 Beta · older

[**FlashFit-AI-Apple-Silicon.zip**](https://github.com/alexmark53343-byte/FlashFit-AI/raw/main/FlashFit-AI-Apple-Silicon.zip) · 1.3 MB · M1, M2, M3, M4 and newer

This is the August 3rd build and it has not moved since. Development has been on
Windows, so it does not have the work that followed — the slicer import fix, the
local AI, the wider filament catalogue, or the corrected time estimates. The
sources in this repository no longer build it either: on anything other than
Windows they compile to the engine's self-check and nothing else.

It is kept here because it still works for what 3.6 did. Use the Windows build
if you have the choice.

---

## What it does

**Reads the actual mesh.** Dimensions, triangle count, watertightness,
degenerate faces, overhang ratio, bed contact, how solid the part is compared to
its bounding box, and how many separate pieces the file holds. Every number the
app acts on comes from the geometry, not from the file name.

**Finds your printer and its profiles.** It locates the installed slicer and
reads its machine, process and filament profiles — including the same printer at
different nozzle sizes, listed separately so 0.25, 0.4, 0.6 and 0.8 mm are each
selectable and the incompatible process profiles are filtered out.

**Recognises what the part is.** A small language model runs locally, on CPU, in
its own process. It is given the file name and a set of shape descriptors and
answers with what the part is and which class it belongs to — hollow shell,
decorative, mechanical, slender. It never chooses settings.

**Decides the settings deterministically.** The recognised class, the quality
tier and your stated priority produce the adjustments in code, so the same part
always gives the same profile. This division is deliberate: measured against
real cases, a small model identifies parts reliably and reasons about numbers
badly.

**Repairs the finished profile before you commit.** Settings are chosen one at a
time but a print fails on how they combine, so the whole set is walked against
the machine and the material: ringing from acceleration on tall parts, flow
beyond what the filament can melt, bridges laid faster than they can set,
cooling time on fine layers, a layer height the fitted nozzle cannot lay,
temperatures outside either limit. Anything predicted is corrected and the
profile is checked again, and the slicer only opens once it comes back clean.

**Splits a model that does not fit onto more plates.** A download is often
several separate parts sharing one file. Rather than refusing it, or shrinking
it, the geometry is separated into its actually disconnected pieces and packed
over as many plates as the machine needs — at full size. Parts welded into a
single solid — a boolean union, a print-in-place assembly, pieces on a sprue —
are recognised by the pinch at their join and separated there, with both cut
faces closed.

**Writes a project the slicer opens with the settings applied.** Built on top of
the installed profiles, with the FlashFit values laid over them, so vendor
G-code, retraction, Pressure Advance, purge and calibration all survive.

---

## Guardrails

The principle behind the whole project: the app must not be able to produce an
unsafe or nonsensical profile, not even by mistake.

- Vendor profiles remain the authority. Machine G-code, retraction, Flow
  Dynamics / Pressure Advance, purge, wipe and calibration are never touched.
- Every supported printer carries hard physical ceilings — build volume, maximum
  speed, acceleration, hotend and bed temperature.
- Only a closed list of parameters can be modified. Anything outside it fails
  the build rather than being written.
- Every generated profile is read back from disk and re-verified.
- The produced 3MF is validated: geometry compared against the analysed model,
  and the settings confirmed present.

**The AI has no authority over any of this.** It proposes a classification; a
veto in code decides what survives. Advice that would change the layer height,
speed the machine up, leave the envelope or exceed the tier's time budget is
discarded whole. Advice that is merely too expensive is scaled back until it
fits, rather than thrown away. With no model present, the results are identical
to the deterministic path — there is a test that asserts exactly that.

### The three layers

| | Does what | Cannot do |
|---|---|---|
| **Model** | Says what the part is, how much its surface will be looked at, and which known problems it is prone to | Choose any number |
| **Guardrail** | Decides whether to believe it — the class is checked against the measured mesh, and one the geometry denies is withdrawn while the name is kept | Let an off-contract answer through |
| **S.O.G** | *Security On Guardrail.* Corrects the finished profile, re-checks, and clears the print only after its last change | Make anything faster or hotter |

The guardrail's checks used to be one-sided: they refused advice for asking too
much, so a misclassification that made the print *worse* passed untouched —
"hollow" on a solid bracket removes infill and slows down, which is safe by every
rule and wrong. The class is now verified against the mesh, and solidity is
ignored outright when the mesh is not closed, because a mesh with holes has no
volume and every number derived from it is an artefact.

Each layer reports for itself in the checks panel, and each row is always
present — a layer that is only visible when it objects cannot be told apart from
one that is not running. The model row says what it recognised, the guardrail
row says whether that was believed, and the S.O.G row says what happened to the
profile.

S.O.G's use of the model is **monotone**: every answer it can give either leaves
a safety margin where it was or widens it. The worst case of a wrong answer is a
print more careful than it needed to be. Both layers read the same machine
manual, so a limit has one definition rather than one per file.

---

## The local AI

The application is 7 MB. Nothing is embedded: a gigabyte inside the executable
is a gigabyte in every clone of this repository and every download by someone
who only wanted the app.

Two buttons in the toolbar choose between a **light** model and a **stronger**
one. A model not yet on disk is marked with an arrow, and choosing it downloads
it — the engine first, then the weights, with a real percentage and transfer
size, resumable if it is interrupted. Weights already present are found by size,
so anything fetched by hand or by an earlier version is reused rather than
downloaded again.

| | Model | Size | Memory |
|---|---|---|---|
| Light | Qwen2.5 1.5B Q4 | ~1.0 GB | runs anywhere |
| Strong | Qwen2.5 3B Q4 | ~2.0 GB | wants a roomier machine |

A status chip reports what the model is doing — downloading, loading with the
percentage the server itself reports, online, working, or unavailable. Only the
"working" state animates, so a still dot always means idle.

**The choice does not affect print quality.** The settings are computed in code
either way; a stronger model recognises more unusual parts, nothing more. That
is what makes the light one a real option on a machine with little memory rather
than a downgrade. If the chosen weights would not fit in free memory, the app
falls back rather than swapping the machine to a standstill.

Everything runs offline on CPU, in a child process inside a Windows job object
with kill-on-close, so the server can never be left behind — not by a crash, not
by a forced quit.

---

## Requirements

- Windows 11 x64 for the current build, or an Apple Silicon Mac for the older
  3.6 one
- Flash Studio Desktop, Bambu Studio, or a compatible Orca build, with an
  official profile installed for your printer
- No GPU required. The model runs on CPU and uses a GPU only if one is present.

Installations whose slicer exposes no command line are supported: the project is
written directly and opened, and you press Slice yourself.

---

## Supported printers

**Flashforge** — Creator 5, Creator 5 Pro, Adventurer 5M, Adventurer 5M Pro,
AD5X, Guider 3 Ultra

**Bambu Lab** — A1 mini, A1, A2L, P1P, P1S, P2S, X1, X1 Carbon, X1E, X2D, H2C,
H2D, H2D Pro, H2S

Single-material workflow. Vendor multi-material mechanics are left untouched.

---

## Interface

Native Win32 and GDI: no Electron, no browser, no GPU runtime. Light and dark
themes, five languages (Italian, English, French, Spanish, German), an
orbitable software-rendered view of the real geometry, and a status bar carrying
filament length, duration and weight.

---

## Estimates

Time and weight are estimates, and they are labelled as such. The time model is
calibrated against real sliced results and is currently within roughly ten
percent on the prints it has been checked against; it should be treated as a
guide, not as the slicer's own number. Weight uses the density of the filament
actually selected.

---

## Building

```
go build -trimpath -ldflags="-s -w -H=windowsgui" -o "FlashFit AI.exe" ./appnative
```

That is the whole build: about 7 MB, no assets to fetch first. The model and the
llama.cpp engine are downloaded by the application when a model is first chosen,
into `%APPDATA%\FlashFitAI\models`.

Run the end-to-end self-check with:

```
go run ./appnative --self-test
```

---

## Status

Beta. Inspect the generated project, the sliced time and the layer preview
before printing.

The executable is not yet Authenticode-signed, so Windows SmartScreen will show
an unknown publisher. Download only from this repository.
