# FlashFit AI

FlashFit AI reads a 3D model, works out how it should be printed on **your**
machine with **your** filament, and hands the slicer a project that is already
set up. It stops one step before slicing: the slicer still does the slicing.

It is an independent project by a single Italian developer. It began with a
simple frustration — advanced slicers are powerful, but finding the right
combination of settings for each model and material is still needlessly hard.

**Windows only.** The macOS build is discontinued and no longer supported.

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

**Checks the finished profile before you commit.** Settings are chosen one at a
time but a print fails on how they combine, so the whole set is walked against
the machine and the material: ringing from acceleration on tall parts, flow
beyond what the filament can melt, bridges laid faster than they can set,
cooling time on fine layers, temperatures outside either limit. A predicted
defect costs nothing; the same defect found on the plate costs the print.

**Splits a model that does not fit onto more plates.** A download is often
several separate parts sharing one file. Rather than refusing it, or shrinking
it, the geometry is separated into its actually disconnected pieces and packed
over as many plates as the machine needs — at full size.

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

---

## The local AI

A light model is built into the application and works out of the box, offline.
There is nothing to install and nothing to configure.

Two buttons in the toolbar switch between the **light** model and a **stronger**
one, if you have placed heavier weights in the models folder. A status chip
shows what the model is doing: loading, online, working, or unavailable — and
only the "working" state animates, so a still dot always means idle.

The choice does not affect print quality. The settings are computed in code
either way; a stronger model simply recognises more unusual parts. That is what
makes the light model a real option on a machine with little memory rather than
a downgrade.

The model server runs as a child process inside a Windows job object with
kill-on-close, so it can never be left behind — not by a crash, not by a forced
quit.

---

## Requirements

- Windows 11 x64
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
go build -tags embedmodel -trimpath -ldflags="-s -w -H=windowsgui" -o "FlashFit AI.exe" ./appnative
```

The `embedmodel` tag embeds the language model, which the release build needs
and which makes the binary about a gigabyte. Without the tag the build is a few
megabytes and looks for weights in the models folder instead — that is the build
to use while developing.

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
