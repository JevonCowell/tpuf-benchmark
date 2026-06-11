package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/turbopuffer/tpuf-benchmark/pkg/output"
	"github.com/turbopuffer/turbopuffer-go"
)

// loadExistingNamespaces loads preseeded namespaces and validates query compatibility.
func loadExistingNamespaces(
	ctx context.Context,
	client *turbopuffer.Client,
	def *Definition,
	names []string,
	logger *output.Logger,
) ([]*Namespace, []int, error) {
	if len(names) == 0 {
		return nil, nil, errors.New("existing namespace list must not be empty")
	}
	logger.Detailf("using %d existing namespace(s); initial setup/seeding disabled", len(names))
	if len(def.Workloads.Upsert) > 0 {
		logger.Detailf("runtime upsert workloads are still enabled for existing namespaces")
	}

	declaredSchema, hasDeclaredSchema, err := declaredSetupSchema(def)
	if err != nil {
		return nil, nil, err
	}

	namespaces := make([]*Namespace, len(names))
	sizes := make([]int, len(names))
	for i, name := range names {
		ns := NewNamespace(ctx, client, name)
		metadata, err := ns.Metadata(ctx)
		if err != nil {
			return nil, nil, namespaceMetadataError(name, err)
		}
		if metadata.ApproxRowCount > int64(^uint(0)>>1) {
			return nil, nil, fmt.Errorf("existing namespace %q size too large: %d", name, metadata.ApproxRowCount)
		}
		if hasDeclaredSchema {
			if err := checkSchemaCompatibility(declaredSchema, metadata.Schema); err != nil {
				return nil, nil, fmt.Errorf("existing namespace %q schema is incompatible: %w", name, err)
			}
		}
		namespaces[i] = ns
		sizes[i] = int(metadata.ApproxRowCount)
	}

	if err := preflightQueries(ctx, namespaces, def.Workloads.Query); err != nil {
		return nil, nil, err
	}
	return namespaces, sizes, nil
}

// namespaceMetadataError wraps namespace metadata errors with existence context.
func namespaceMetadataError(name string, err error) error {
	var apiErr *turbopuffer.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return fmt.Errorf("existing namespace %q does not exist", name)
	}
	return fmt.Errorf("getting metadata for existing namespace %q: %w", name, err)
}

// declaredSetupSchema extracts the optional schema from the setup upsert template.
func declaredSetupSchema(def *Definition) (map[string]json.RawMessage, bool, error) {
	var upsertBuf bytes.Buffer
	if err := def.Setup.UpsertTemplate.Execute(&upsertBuf, struct {
		UpsertBatchPlaceholder string
	}{
		UpsertBatchPlaceholder: "",
	}); err != nil {
		return nil, false, fmt.Errorf("rendering setup upsert template for schema validation: %w", err)
	}
	return extractDeclaredSchema(upsertBuf.Bytes())
}

// extractDeclaredSchema extracts the optional schema object from an upsert body.
func extractDeclaredSchema(body []byte) (map[string]json.RawMessage, bool, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, false, nil
	}

	var payload struct {
		Schema map[string]json.RawMessage `json:"schema"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("parsing setup upsert template for schema validation: %w", err)
	}
	if len(payload.Schema) == 0 {
		return nil, false, nil
	}
	return payload.Schema, true, nil
}

// checkSchemaCompatibility verifies declared schema fields exist with matching config.
func checkSchemaCompatibility(declared map[string]json.RawMessage, actual map[string]turbopuffer.AttributeSchemaConfig) error {
	for name, expected := range declared {
		actualConfig, ok := actual[name]
		if !ok {
			return fmt.Errorf("declared schema field %q is missing", name)
		}
		actualJSON := actualConfig.RawJSON()
		if actualJSON == "" {
			actualBytes, err := json.Marshal(actualConfig)
			if err != nil {
				return fmt.Errorf("marshaling actual schema field %q: %w", name, err)
			}
			actualJSON = string(actualBytes)
		}
		if !jsonEqual(expected, []byte(actualJSON)) {
			return fmt.Errorf("declared schema field %q does not match existing schema", name)
		}
	}
	return nil
}

// jsonEqual compares JSON values after normalizing object key order.
func jsonEqual(a, b []byte) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return jsonSubset(av, bv)
}

// jsonSubset reports whether expected is contained in actual.
func jsonSubset(expected, actual any) bool {
	expectedMap, expectedIsMap := expected.(map[string]any)
	actualMap, actualIsMap := actual.(map[string]any)
	if expectedIsMap {
		if !actualIsMap {
			return false
		}
		for key, expectedValue := range expectedMap {
			actualValue, ok := actualMap[key]
			if !ok {
				return false
			}
			if (key == "ann" || key == "full_text_search") && expectedValue == true {
				if actualValue == true {
					continue
				}
				if _, ok := actualValue.(map[string]any); ok {
					continue
				}
			}
			if !jsonSubset(expectedValue, actualValue) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(expected, actual)
}

// preflightQueries runs one real query per workload against each namespace.
func preflightQueries(ctx context.Context, namespaces []*Namespace, queryWorkloads map[string]*QueryWorkload) error {
	workloadNames := sortedKeys(queryWorkloads)
	for _, ns := range namespaces {
		for _, workloadName := range workloadNames {
			workload := queryWorkloads[workloadName]
			if _, _, err := ns.Query(ctx, workload.MaxRetries, workload.QueryTemplate); err != nil {
				return fmt.Errorf("preflight query failed for namespace %q workload %q: %w", ns.ID(), workloadName, err)
			}
		}
	}
	return nil
}
