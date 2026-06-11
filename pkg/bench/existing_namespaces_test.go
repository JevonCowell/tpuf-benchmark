package bench

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/turbopuffer/turbopuffer-go"
)

func TestExtractDeclaredSchema(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    map[string]string
		wantOK  bool
		wantErr bool
	}{
		{
			name:   "extracts schema",
			body:   `{"upsert_rows": [], "schema": {"text": {"type": "string", "full_text_search": true}}}`,
			want:   map[string]string{"text": `{"type": "string", "full_text_search": true}`},
			wantOK: true,
		},
		{
			name:   "empty body",
			body:   ``,
			wantOK: false,
		},
		{
			name:   "no schema",
			body:   `{"upsert_rows": []}`,
			wantOK: false,
		},
		{
			name:    "malformed json",
			body:    `{`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := extractDeclaredSchema([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("extractDeclaredSchema: %v", err)
			}
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			for field, wantJSON := range tt.want {
				if !jsonEqual(got[field], []byte(wantJSON)) {
					t.Fatalf("schema[%s] = %s, want %s", field, got[field], wantJSON)
				}
			}
		})
	}
}

func TestCheckSchemaCompatibility(t *testing.T) {
	actual := map[string]turbopuffer.AttributeSchemaConfig{
		"text":   mustSchemaConfig(t, `{"type": "string", "full_text_search": true, "filterable": true}`),
		"vector": mustSchemaConfig(t, `{"type": "[1024]f16", "ann": {"distance_metric": "cosine_distance"}}`),
		"extra":  mustSchemaConfig(t, `{"type": "string"}`),
	}

	tests := []struct {
		name     string
		declared map[string]string
		wantErr  string
	}{
		{
			name: "allows exact and extra actual fields",
			declared: map[string]string{
				"text": `{"type": "string", "full_text_search": true}`,
			},
		},
		{
			name: "missing field",
			declared: map[string]string{
				"missing": `{"type": "string"}`,
			},
			wantErr: "missing",
		},
		{
			name: "type mismatch",
			declared: map[string]string{
				"text": `{"type": "int"}`,
			},
			wantErr: "text",
		},
		{
			name: "ann true allows configured ann object",
			declared: map[string]string{
				"vector": `{"type": "[1024]f16", "ann": true}`,
			},
		},
		{
			name: "full text true allows configured full text object",
			declared: map[string]string{
				"text": `{"type": "string", "full_text_search": true}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			declared := make(map[string]json.RawMessage)
			for field, value := range tt.declared {
				declared[field] = json.RawMessage(value)
			}
			err := checkSchemaCompatibility(declared, actual)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("checkSchemaCompatibility: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func mustSchemaConfig(t *testing.T, raw string) turbopuffer.AttributeSchemaConfig {
	t.Helper()
	var cfg turbopuffer.AttributeSchemaConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal schema config: %v", err)
	}
	return cfg
}
