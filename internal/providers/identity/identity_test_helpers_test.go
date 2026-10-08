package identity

import "encoding/json"

func jsonUnmarshalImpl(b []byte, v any) error { return json.Unmarshal(b, v) }
