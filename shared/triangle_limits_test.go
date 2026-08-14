package shared

import "testing"

// The same mesh must be judged the same way whatever file it arrives in. The
// 3MF path used to allow half of what the STL path allowed, so a 750k-triangle
// model was accepted as an STL and refused as a 3MF — identical geometry, two
// different answers, and an error message that blamed the user's mesh.
func TestTriangleLimitsAgreeAcrossFormats(t *testing.T) {
	if MaxSanitizedTriangles != MaxTriangles {
		t.Fatalf("limiti divergenti: 3MF %d, STL/OBJ %d — la stessa mesh verrebbe accettata in un formato e rifiutata nell'altro",
			MaxSanitizedTriangles, MaxTriangles)
	}
}

// The limit has to leave room for the models people actually print. A detailed
// car sits around three quarters of a million triangles.
func TestTriangleLimitFitsDetailedModels(t *testing.T) {
	const detailedCarTriangles = 746_638
	if MaxTriangles < detailedCarTriangles {
		t.Fatalf("limite %d troppo basso: un modello dettagliato comune ne ha %d", MaxTriangles, detailedCarTriangles)
	}
}
