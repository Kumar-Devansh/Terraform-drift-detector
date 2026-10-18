package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/driftctl/driftctl/internal/model"
	"github.com/driftctl/driftctl/internal/store"
)

func TestScannerRunSkipCloud(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	statePath, _ := filepath.Abs("../../testdata/state/sample.tfstate")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatal(err)
	}

	scanner := NewScanner(st)
	report, err := scanner.Run(context.Background(), Options{
		WorkspaceName: "test",
		Provider:      "aws",
		State:         model.StateConfig{Backend: "local", Path: statePath},
		SkipCloud:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != model.ScanStatusCompleted {
		t.Fatalf("expected completed, got %s", report.Status)
	}
	if report.Summary.TotalResources != 3 {
		t.Fatalf("expected 3 resources, got %d", report.Summary.TotalResources)
	}
	// Without cloud data, all resources appear missing
	if report.Summary.MissingInCloud != 3 {
		t.Fatalf("expected 3 missing, got %d", report.Summary.MissingInCloud)
	}
}
