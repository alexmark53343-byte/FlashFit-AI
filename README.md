# FlashFit AI

FlashFit AI is an Italian project that analyzes 3D models and automatically prepares optimized slicing settings for the Flashforge Adventurer 5M.

It is an independent project created and developed by a single Italian developer. FlashFit AI began with a simple frustration: advanced slicers are powerful, but finding the right combination of settings for every model and filament is still unnecessarily difficult. The project grew from that idea into a focused assistant that studies the geometry first and builds a safer, more appropriate printing strategy around it.

The application inspects the actual mesh—including dimensions, topology, overhangs, surface orientation, fine details, separate bodies, and geometric proportions—then adapts print speed, acceleration, walls, infill, supports, cooling, adhesion, ironing, and material parameters.

## Official downloads

- [Windows 11 x64 — FlashFit AI 3.6.1 Beta](https://github.com/alexmark53343-byte/FlashFit-AI/releases/download/v3.6-beta/FlashFit-AI-Windows-11-x64.zip)
- [macOS Apple Silicon — FlashFit AI 3.6 Beta](https://github.com/alexmark53343-byte/FlashFit-AI/releases/download/v3.6-beta/FlashFit-AI-Apple-Silicon.zip)

## Current release

- Native macOS application for Apple Silicon (M1, M2, M3, M4 and newer)
- Native Windows 11 x64 beta application
- Flashforge Adventurer 5M with a 0.4 mm nozzle
- Single-color and single-extruder workflow
- STL, OBJ, and 3MF analysis
- Fast, Balanced, and Perfect print strategies
- 53 profiles covering PLA, PETG, ABS, and TPU
- English, Italian, French, Spanish, and German interface
- Direct Flash Studio project generation

## Platform availability

FlashFit AI is currently available for **macOS on Apple Silicon** and as an early **Windows 11 x64 beta**.

## Beta notice

This is a free beta. Always review the generated slicing preview before printing. Material behavior can vary between colors, production batches, storage conditions, and individual printers.

ABS can warp and release fumes. Large ABS parts generally require an enclosure and suitable ventilation; software settings cannot replace those physical protections.

## Installation

1. Download `FlashFit-AI-Apple-Silicon.zip` from the latest release.
2. Extract `FlashFit AI.app`.
3. Move the app to the Applications folder.
4. Open a model and verify the generated project in Flash Studio before printing.

### Windows 11

1. Download and extract `FlashFit-AI-Windows-11-x64.zip`.
2. Run `FlashFit-AI-Windows-11.exe`.
3. Windows SmartScreen may warn about an unknown publisher because this independent beta does not yet have a commercial Windows code-signing certificate. Continue only when downloading from this official repository.

## Project origin

Designed and developed in Italy by one independent developer. 🇮🇹

The project is intentionally small and personal. The application and its profile engine are maintained exclusively by its original developer; this public repository distributes compiled beta releases and public documentation only.
