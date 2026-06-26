package agentskill

import (
	"encoding/json"
	"io"
)

func ReadJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
