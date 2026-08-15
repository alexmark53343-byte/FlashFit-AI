package shared

import "encoding/json"

func jsonUnmarshalReal(b []byte, v any) error { return json.Unmarshal(b, v) }
