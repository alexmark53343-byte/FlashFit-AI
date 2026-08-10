# FlashFit AI 4.3 printer engineering support

FlashFit 4.3 does not replace the manufacturer profile. It locates an installed
Flash Studio Desktop, Bambu Studio, or compatible Orca profile and applies only
an audited allowlist of portable print and filament settings.

## Supported 0.4 mm families

| Brand | Models |
| --- | --- |
| Flashforge | Creator 5, Creator 5 Pro, Adventurer 5M, Adventurer 5M Pro, AD5X, Guider 3 Ultra |
| Bambu Lab | A1 mini, A1, A2L, P1P, P1S, P2S, X1, X1 Carbon, X1E, X2D, H2C, H2D, H2D Pro, H2S |

Only a real, readable 0.4 mm machine profile installed with the slicer enables a
printer. A marketing name by itself is not enough. Profiles for 0.2, 0.6, or
0.8 mm nozzles are rejected by this build.

## Engineering rules

- The selected model must fit the exact selected printer volume.
- Filament flow is capped below its documented maximum volumetric speed.
- Short prints keep Fast close to Balanced because sacrificing quality to save
  only a few minutes is not useful.
- Perfect increases detail and shell quality without allowing unbounded print
  times on long jobs.
- Bed-slinger acceleration is reduced for tall or narrow parts.
- Very tall Guider 3 Ultra jobs receive an additional anti-ringing limit.
- Enclosed machines with a known PLA heat-creep risk show a ventilation warning.
- Machine G-code, retraction, kinematics, nozzle wiping, purge logic, tool
  changes and vendor calibration are never rewritten.
- Flow Dynamics/Pressure Advance is inherited unless the user has stored a real
  measured spool calibration in FlashFit.

## Problem research translated into safeguards

Community reports were treated as failure signals, not as authoritative numeric
settings. Reports of enclosure heat creep, VFA/ringing, filament-path jams,
nozzle buildup, first/top-layer gaps, and profiles losing their K factor led to
conservative motion limits, explicit warnings, and preservation of vendor
calibration and wipe/tool-change behavior. Hardware faults still require physical
inspection and cannot be repaired by slicer settings.

## Primary technical sources

- [Flash Studio Desktop supported printers](https://www.flashforge.com/pages/flash-studio-desktop)
- [Flashforge current printer lineup](https://www.flashforge.com/collections/adventurer-series)
- [Flashforge Creator 5 specifications](https://www.flashforge.com/products/flashforge-creator-5)
- [Flashforge AD5X specifications](https://www.flashforge.com/products/flashforge-ad5x-3d-printer)
- [Flashforge Guider 3 Ultra specifications](https://enterprise.flashforge.com/pages/guider-3-ultra-3d-printer)
- [Bambu Lab printer comparison](https://bambulab.com/en-us/compare)
- [Bambu Lab official Bambu Studio machine profiles](https://github.com/bambulab/BambuStudio/tree/master/resources/profiles/BBL/machine)

The final sliced time and layer preview shown by the selected slicer remain the
last authority before printing.
