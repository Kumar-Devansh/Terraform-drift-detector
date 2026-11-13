package state

import (
	"os"
	"testing"
)

func TestExtractorExtract(t *testing.T) {
	data, err := os.ReadFile("../../testdata/state/sample.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	e := NewExtractor("aws")
	resources, err := e.Extract(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(resources))
	}

	byType := make(map[string]int)
	for _, r := range resources {
		byType[r.Type]++
		if r.Provider != "aws" {
			t.Fatalf("expected aws provider, got %s", r.Provider)
		}
		if r.Source != "state" {
			t.Fatalf("expected state source, got %s", r.Source)
		}
	}
	if byType["aws_vpc"] != 1 || byType["aws_instance"] != 1 || byType["aws_s3_bucket"] != 1 {
		t.Fatalf("unexpected resource types: %v", byType)
	}
}

func TestNormalizeS3Versioning(t *testing.T) {
	got := normalizeAttributes("aws_s3_bucket", map[string]any{
		"force_destroy": true,
		"versioning":    []any{map[string]any{"enabled": true, "mfa_delete": false}},
	})
	if _, ok := got["force_destroy"]; ok {
		t.Fatal("force_destroy should not be compared as cloud drift")
	}
	versioning, ok := got["versioning"].(map[string]any)
	if !ok || versioning["enabled"] != true || versioning["mfa_delete"] != false {
		t.Fatalf("unexpected normalized versioning: %#v", got["versioning"])
	}
}
