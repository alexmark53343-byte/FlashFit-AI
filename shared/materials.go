package shared

import "strings"

// Material families.
//
// The built-in catalogue already covers twenty material types, but three places
// in the pipeline only knew about PLA and PETG: profile discovery discarded
// every other filament the slicer had installed, base-profile matching refused
// anything else, and the tuning had a single special case. So a user with ABS
// or TPU profiles sitting on disk saw none of them.
//
// A family is decided by prefix because that is how vendors name things: "PLA",
// "PLA-CF", "PLA MATTE" and "HS PLA" are all PLA and all print like it, with
// differences the individual filament entry carries.

// MaterialFamily is the coarse kind a filament belongs to.
type MaterialFamily string

const (
	FamilyPLA     MaterialFamily = "PLA"
	FamilyPETG    MaterialFamily = "PETG"
	FamilyABS     MaterialFamily = "ABS"  // includes ASA, which behaves the same way
	FamilyTPU     MaterialFamily = "TPU"  // flexible
	FamilyNylon   MaterialFamily = "PA"   // includes PA-CF
	FamilyPC      MaterialFamily = "PC"   // polycarbonate
	FamilySupport MaterialFamily = "PVA"  // soluble and break-away supports
	FamilyOther   MaterialFamily = "ALTRO"
)

// FamilyOf classifies a filament by its declared material.
func FamilyOf(material string) MaterialFamily {
	m := strings.ToUpper(strings.TrimSpace(material))
	// Checked longest-first: PETG must not be caught by the PET rule, and
	// PLA must not swallow PVA.
	switch {
	case strings.Contains(m, "PETG"), strings.Contains(m, "PET-CF"), strings.Contains(m, "PET-GF"):
		return FamilyPETG
	case strings.Contains(m, "TPU"), strings.Contains(m, "TPE"), strings.Contains(m, "FLEX"):
		return FamilyTPU
	case strings.Contains(m, "PVA"), strings.Contains(m, "BVOH"), strings.Contains(m, "HIPS"):
		return FamilySupport
	case strings.Contains(m, "ABS"), strings.Contains(m, "ASA"):
		return FamilyABS
	case strings.HasPrefix(m, "PC"), strings.Contains(m, "POLYCARB"):
		return FamilyPC
	case strings.HasPrefix(m, "PA"), strings.Contains(m, "NYLON"):
		return FamilyNylon
	case strings.Contains(m, "PLA"):
		return FamilyPLA
	default:
		return FamilyOther
	}
}

// SupportedMaterial reports whether the engine has tuning for this family. Only
// families it can print safely are offered.
func SupportedMaterial(material string) bool {
	switch FamilyOf(material) {
	case FamilyPLA, FamilyPETG, FamilyABS, FamilyTPU, FamilyNylon, FamilyPC, FamilySupport:
		return true
	}
	return false
}

// SameFamily reports whether a base profile may serve a filament. Vendors ship
// one base per family, so an ABS profile is the right starting point for ASA
// and a PLA one for PLA-CF.
func SameFamily(want, have string) bool {
	return FamilyOf(want) == FamilyOf(have)
}

// IsAbrasive reports whether a filament carries a fill that wears a brass
// nozzle: carbon, glass, wood, metal and glow fills all do. This is a property
// of the nozzle rather than of the print, which is why it is asked separately
// from whether the material itself is supported.
func IsAbrasive(f Filament) bool {
	haystack := strings.ToUpper(f.Material + " " + f.Product + " " + f.Variant)
	for _, token := range []string{"CF", "CARBON", "GF", "GLASS", "WOOD", "METAL", "GLOW"} {
		// Matched with a separator so "CF" does not fire on a product name that
		// merely happens to contain those two letters.
		if strings.Contains(haystack, " "+token) || strings.Contains(haystack, "-"+token) {
			return true
		}
	}
	return false
}

// MaterialBehaviour is what the tuning needs to know about a family beyond the
// numbers on the individual spool.
type MaterialBehaviour struct {
	// MaxFanPercent caps part cooling. ABS and ASA warp and delaminate when
	// cooled hard; PLA wants all the air it can get.
	MaxFanPercent int
	// NeedsBrim is true for families that lift off the plate as they contract.
	NeedsBrim bool
	// SpeedCeiling limits how fast the outer wall may run regardless of flow:
	// flexibles buckle in the extruder long before they run out of melt.
	SpeedCeiling float64
	// BridgeCeiling is how fast a bridge may be laid before it droops.
	BridgeCeiling float64
	// FirstLayerBoost thickens the first layer for families that need the extra
	// grip; 1.0 leaves it alone.
	FirstLayerBoost float64
	// Enclosed is true when the family really wants a closed chamber. It does
	// not block printing, it warns.
	Enclosed bool
}

// BehaviourOf returns the handling rules for a material.
func BehaviourOf(material string) MaterialBehaviour {
	switch FamilyOf(material) {
	case FamilyPETG:
		// Stays soft longer than PLA: strings if cooled too little, droops on
		// bridges if laid too fast.
		return MaterialBehaviour{MaxFanPercent: 60, SpeedCeiling: 0, BridgeCeiling: 22, FirstLayerBoost: 1.0}
	case FamilyABS:
		// Warps as it contracts. Cooling is the enemy, adhesion is everything.
		return MaterialBehaviour{MaxFanPercent: 20, NeedsBrim: true, SpeedCeiling: 90, BridgeCeiling: 25, FirstLayerBoost: 1.1, Enclosed: true}
	case FamilyTPU:
		// The limit is the extruder, not the hotend: push a flexible too fast
		// and it buckles instead of feeding.
		return MaterialBehaviour{MaxFanPercent: 50, SpeedCeiling: 30, BridgeCeiling: 15, FirstLayerBoost: 1.0}
	case FamilyNylon:
		// Hygroscopic and prone to warping; wants heat and little air.
		return MaterialBehaviour{MaxFanPercent: 25, NeedsBrim: true, SpeedCeiling: 80, BridgeCeiling: 20, FirstLayerBoost: 1.1, Enclosed: true}
	case FamilyPC:
		// The most warp-prone of the common engineering materials.
		return MaterialBehaviour{MaxFanPercent: 15, NeedsBrim: true, SpeedCeiling: 70, BridgeCeiling: 20, FirstLayerBoost: 1.15, Enclosed: true}
	case FamilySupport:
		// Printed alongside the part, so it follows the part's pace.
		return MaterialBehaviour{MaxFanPercent: 70, SpeedCeiling: 45, BridgeCeiling: 20, FirstLayerBoost: 1.0}
	default: // PLA
		return MaterialBehaviour{MaxFanPercent: 100, SpeedCeiling: 0, BridgeCeiling: 30, FirstLayerBoost: 1.0}
	}
}
