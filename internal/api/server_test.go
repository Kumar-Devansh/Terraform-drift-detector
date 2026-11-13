package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/driftctl/driftctl/internal/config"
	"github.com/driftctl/driftctl/internal/model"
	"github.com/driftctl/driftctl/internal/notify"
	"github.com/driftctl/driftctl/internal/providers"
	"github.com/driftctl/driftctl/internal/scan"
	"github.com/driftctl/driftctl/internal/store"
)

type apiTestCloudProvider struct{}

func (apiTestCloudProvider) Name() string             { return "aws" }
func (apiTestCloudProvider) SupportedTypes() []string { return nil }
func (apiTestCloudProvider) FetchResources(context.Context, []model.Resource, []string, []string) ([]model.Resource, error) {
	return nil, nil
}

type apiTestEmailNotifier struct{}

func (apiTestEmailNotifier) Name() string                                   { return "email" }
func (apiTestEmailNotifier) Send(context.Context, *model.DriftReport) error { return nil }

func TestDashboardAndStaticRoutes(t *testing.T) {
	_, sourceFile, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	// Constructing the server registers every route and will panic if any
	// ServeMux patterns conflict.
	server := NewServer(config.APIConfig{}, nil, nil, nil)

	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/", want: http.StatusOK},
		{path: "/static/style.css", want: http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		if res.Code != test.want {
			t.Errorf("GET %s returned %d, want %d", test.path, res.Code, test.want)
		}
	}
}

func TestAPIRoutePatternsRemainRegistered(t *testing.T) {
	server := NewServer(config.APIConfig{}, nil, nil, nil)
	for _, test := range []struct{ method, path string }{
		{http.MethodGet, "/health"},
		{http.MethodGet, "/api/v1/workspaces"},
		{http.MethodPost, "/api/v1/workspaces"},
		{http.MethodGet, "/api/v1/workspaces/ws-1"},
		{http.MethodDelete, "/api/v1/workspaces/ws-1"},
		{http.MethodPost, "/api/v1/workspaces/ws-1/scans"},
		{http.MethodGet, "/api/v1/workspaces/ws-1/scans"},
		{http.MethodGet, "/api/v1/scans"},
		{http.MethodGet, "/api/v1/scans/scan-1"},
		{http.MethodGet, "/api/v1/scans/scan-1/report"},
		{http.MethodPut, "/api/v1/workspaces/ws-1/schedules"},
		{http.MethodDelete, "/api/v1/workspaces/ws-1/schedules"},
	} {
		_, pattern := server.mux.Handler(httptest.NewRequest(test.method, test.path, nil))
		if pattern == "" {
			t.Errorf("no route registered for %s %s", test.method, test.path)
		}
	}
}

func TestScanAPIReturnsAndPersistsEmailNotificationStatus(t *testing.T) {
	_, sourceFile, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "api-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	workspace := &model.Workspace{
		Name: "api-test", Provider: "aws",
		State: model.StateConfig{Backend: "local", Path: filepath.Join(root, "testdata", "state", "sample.tfstate")},
	}
	if err := st.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	scanner := scan.NewScanner(st)
	scanner.Providers = providers.NewRegistry()
	scanner.Providers.Register(apiTestCloudProvider{})
	scanner.Notifiers = []notify.Notifier{apiTestEmailNotifier{}}
	server := NewServer(config.APIConfig{}, st, scanner, nil)

	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/scans", workspace.ID), nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("scan API returned %d: %s", response.Code, response.Body.String())
	}
	var report model.DriftReport
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.EmailNotificationStatus != model.EmailNotificationSent {
		t.Fatalf("expected API email status %q, got %q", model.EmailNotificationSent, report.EmailNotificationStatus)
	}
	if got := response.Body.String(); strings.Contains(got, "password") || strings.Contains(got, "smtp.example") {
		t.Fatalf("scan API exposed SMTP configuration: %s", got)
	}

	saved, err := st.GetScan(context.Background(), report.ScanID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.EmailNotificationStatus != model.EmailNotificationSent {
		t.Fatalf("expected saved email status %q, got %q", model.EmailNotificationSent, saved.EmailNotificationStatus)
	}
}
