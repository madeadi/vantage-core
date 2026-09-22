package agentsdk

import (
	"encoding/json"
	"testing"
)

type exampleTelemetryA struct {
	BatteryPercent float64 `json:"battery_percent"`
	Status         string  `json:"status"`
}

type exampleTelemetryB struct {
	PoseX float64 `json:"x"`
	PoseY float64 `json:"y"`
}

func TestDeriveTelemetrySchemaIsStable(t *testing.T) {
	first, err := DeriveTelemetrySchema[exampleTelemetryA]()
	if err != nil {
		t.Fatalf("DeriveTelemetrySchema: %v", err)
	}
	second, err := DeriveTelemetrySchema[exampleTelemetryA]()
	if err != nil {
		t.Fatalf("DeriveTelemetrySchema (second call): %v", err)
	}

	if first.Hash != second.Hash {
		t.Errorf("hash not stable across calls: %q != %q", first.Hash, second.Hash)
	}
	if string(first.Schema) != string(second.Schema) {
		t.Errorf("schema not stable across calls:\n%s\nvs\n%s", first.Schema, second.Schema)
	}
	if len(first.Hash) != 16 {
		t.Errorf("hash length = %d, want 16", len(first.Hash))
	}
}

func TestDeriveTelemetrySchemaDiffersByType(t *testing.T) {
	a, err := DeriveTelemetrySchema[exampleTelemetryA]()
	if err != nil {
		t.Fatalf("DeriveTelemetrySchema[A]: %v", err)
	}
	b, err := DeriveTelemetrySchema[exampleTelemetryB]()
	if err != nil {
		t.Fatalf("DeriveTelemetrySchema[B]: %v", err)
	}

	if a.Hash == b.Hash {
		t.Errorf("structurally different types produced the same hash: %q", a.Hash)
	}
}

func TestDeriveTelemetrySchemaProducesValidJSON(t *testing.T) {
	s, err := DeriveTelemetrySchema[exampleTelemetryA]()
	if err != nil {
		t.Fatalf("DeriveTelemetrySchema: %v", err)
	}

	var v map[string]any
	if err := json.Unmarshal(s.Schema, &v); err != nil {
		t.Fatalf("Schema is not valid JSON: %v", err)
	}
	if v["type"] != "object" {
		t.Errorf(`Schema["type"] = %v, want "object"`, v["type"])
	}
	props, ok := v["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Schema has no properties object: %v", v)
	}
	if _, ok := props["battery_percent"]; !ok {
		t.Errorf("Schema properties missing battery_percent: %v", props)
	}
	if _, ok := props["status"]; !ok {
		t.Errorf("Schema properties missing status: %v", props)
	}
}

func TestCanonicalizeJSONSortsKeys(t *testing.T) {
	unsorted := []byte(`{"z":1,"a":2,"m":{"y":1,"b":2}}`)
	got, err := canonicalizeJSON(unsorted)
	if err != nil {
		t.Fatalf("canonicalizeJSON: %v", err)
	}
	// json.Marshal on a map[string]any sorts keys, so this ordering is
	// guaranteed by encoding/json, not a property of our own code.
	want := `{"a":2,"m":{"b":2,"y":1},"z":1}`
	if string(got) != want {
		t.Errorf("canonicalizeJSON(%s) = %s, want %s", unsorted, got, want)
	}
}

func TestCanonicalizeJSONInvalidInput(t *testing.T) {
	if _, err := canonicalizeJSON([]byte("not json")); err == nil {
		t.Error("canonicalizeJSON with invalid JSON: want error, got nil")
	}
}
