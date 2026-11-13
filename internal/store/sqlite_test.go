package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/driftctl/driftctl/internal/model"
)

func TestMigrateAddsEmailNotificationStatusToExistingScans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE scans (
		id TEXT PRIMARY KEY, workspace_id TEXT, workspace_name TEXT,
		status TEXT NOT NULL, started_at TEXT NOT NULL, completed_at TEXT,
		summary_json TEXT, findings_json TEXT, errors_json TEXT
	)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	report := &model.DriftReport{
		ScanID: "scan-1", Status: model.ScanStatusCompleted,
		StartedAt: time.Now().UTC(), EmailNotificationStatus: model.EmailNotificationSent,
	}
	if err := st.SaveScan(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.GetScan(context.Background(), report.ScanID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EmailNotificationStatus != model.EmailNotificationSent {
		t.Fatalf("expected persisted status %q, got %q", model.EmailNotificationSent, loaded.EmailNotificationStatus)
	}
}

func TestDeleteWorkspaceRemovesOnlyItsConfigurationScheduleAndScanHistory(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "workspace-delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	prod := &model.Workspace{Name: "prod", Provider: "aws"}
	production := &model.Workspace{Name: "production", Provider: "aws"}
	for _, workspace := range []*model.Workspace{prod, production} {
		if err := st.SaveWorkspace(ctx, workspace); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveSchedule(ctx, workspace.ID, "0 */6 * * *"); err != nil {
			t.Fatal(err)
		}
	}
	for id, workspace := range map[string]*model.Workspace{"scan-prod": prod, "scan-production": production} {
		if err := st.SaveScan(ctx, &model.DriftReport{ScanID: id, WorkspaceID: workspace.ID, Workspace: workspace.Name, Status: model.ScanStatusCompleted}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.DeleteWorkspace(ctx, production.ID); err != nil {
		t.Fatal(err)
	}

	workspaces, err := st.ListWorkspaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != prod.ID || workspaces[0].Name != "prod" {
		t.Fatalf("deleting production affected another workspace: %+v", workspaces)
	}
	if _, err := st.GetScan(ctx, "scan-production"); err == nil {
		t.Fatal("expected production scan history to be removed")
	}
	if _, err := st.GetScan(ctx, "scan-prod"); err != nil {
		t.Fatalf("prod scan history should be preserved: %v", err)
	}
	if _, err := st.GetSchedule(ctx, production.ID); err == nil {
		t.Fatal("expected production schedule to be removed")
	}
	if cron, err := st.GetSchedule(ctx, prod.ID); err != nil || cron != "0 */6 * * *" {
		t.Fatalf("prod schedule should be preserved, got cron=%q err=%v", cron, err)
	}
}
