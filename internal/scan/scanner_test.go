package scan

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/driftctl/driftctl/internal/model"
	"github.com/driftctl/driftctl/internal/notify"
	"github.com/driftctl/driftctl/internal/providers"
	"github.com/driftctl/driftctl/internal/store"
)

type fakeNotifier struct {
	calls int
	err   error
	check func(*model.DriftReport) error
}

type safeNotificationError string

func (e safeNotificationError) Error() string       { return string(e) }
func (e safeNotificationError) SafeMessage() string { return string(e) }

func (n *fakeNotifier) Name() string { return "email" }
func (n *fakeNotifier) Send(_ context.Context, report *model.DriftReport) error {
	n.calls++
	if n.check != nil {
		if err := n.check(report); err != nil {
			return err
		}
	}
	return n.err
}

type expectedResourcesProvider struct{}

func (expectedResourcesProvider) Name() string             { return "aws" }
func (expectedResourcesProvider) SupportedTypes() []string { return nil }
func (expectedResourcesProvider) FetchResources(_ context.Context, expected []model.Resource, _, _ []string) ([]model.Resource, error) {
	return expected, nil
}

type scopedResourcesProvider struct {
	actual []model.Resource
	scope  []string
}

func (p *scopedResourcesProvider) Name() string             { return "aws" }
func (p *scopedResourcesProvider) SupportedTypes() []string { return nil }
func (p *scopedResourcesProvider) FetchResources(_ context.Context, _ []model.Resource, _, scope []string) ([]model.Resource, error) {
	p.scope = append([]string(nil), scope...)
	return p.actual, nil
}

func TestResourceTypeScopeExcludesSecurityGroups(t *testing.T) {
	resources := []model.Resource{
		{Type: "aws_s3_bucket"},
		{Type: "aws_instance"},
		{Type: "aws_vpc"},
		{Type: "aws_subnet"},
		{Type: "aws_security_group"},
	}
	scope := []string{"aws_s3_bucket", "aws_instance", "aws_vpc", "aws_subnet"}
	filtered := filterResourceTypes(resources, scope)
	if len(filtered) != 4 {
		t.Fatalf("expected four in-scope resources, got %d", len(filtered))
	}
	for _, resource := range filtered {
		if resource.Type == "aws_security_group" {
			t.Fatalf("security group was retained in scoped scan: %+v", resource)
		}
	}
}

func TestExcludedSecurityGroupCannotAppearInScanFindings(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()
	statePath := filepath.Join(t.TempDir(), "state.tfstate")
	state := `{"version":4,"resources":[{"mode":"managed","type":"aws_security_group","name":"old","provider":"aws","instances":[{"attributes":{"id":"sg-123","name":"launch-wizard-1","description":"old"}}]},{"mode":"managed","type":"aws_instance","name":"app","provider":"aws","instances":[{"attributes":{"id":"i-123","instance_type":"t3.micro","ami":"ami-123"}}]}]}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &scopedResourcesProvider{actual: []model.Resource{
		{ID: "aws/aws_security_group/sg-extra", Type: "aws_security_group", Name: "launch-wizard-2"},
		{ID: "aws/aws_instance/i-extra", Type: "aws_instance", Name: "unmanaged-instance"},
	}}
	scanner := NewScanner(st)
	scanner.Providers = providers.NewRegistry()
	scanner.Providers.Register(provider)
	scope := []string{"aws_s3_bucket", "aws_instance", "aws_vpc", "aws_subnet"}
	report, err := scanner.Run(context.Background(), Options{
		WorkspaceName: "prod", Provider: "aws",
		State:         model.StateConfig{Backend: "local", Path: statePath},
		ResourceTypes: scope,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.scope) != len(scope) {
		t.Fatalf("provider received scope %v, expected %v", provider.scope, scope)
	}
	for _, finding := range report.Findings {
		if finding.ResourceType == "aws_security_group" {
			t.Fatalf("excluded Security Group appeared in findings: %+v", finding)
		}
	}
	if report.Summary.TotalResources != 1 {
		t.Fatalf("expected only the in-scope Terraform instance, got %d resources", report.Summary.TotalResources)
	}
}

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

func TestScannerNotifiesOnlyAfterDriftReportIsSaved(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()
	scanner := NewScanner(st)
	checker := &fakeNotifier{check: func(report *model.DriftReport) error {
		saved, err := st.GetScan(context.Background(), report.ScanID)
		if err != nil {
			return err
		}
		if saved.Status != model.ScanStatusCompleted || saved.Summary.TotalFindings == 0 {
			return errors.New("scan was not finalized before notification")
		}
		return nil
	}}
	scanner.Notifiers = []notify.Notifier{checker}
	report, err := scanner.Run(context.Background(), testOptions(true))
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.TotalFindings == 0 || checker.calls != 1 {
		t.Fatalf("expected one notification for drift, findings=%d calls=%d", report.Summary.TotalFindings, checker.calls)
	}
	if report.EmailNotificationStatus != model.EmailNotificationSent {
		t.Fatalf("expected email status sent, got %q", report.EmailNotificationStatus)
	}
	saved, err := st.GetScan(context.Background(), report.ScanID)
	if err != nil || saved.EmailNotificationStatus != model.EmailNotificationSent {
		t.Fatalf("expected persisted email status sent, report=%+v err=%v", saved, err)
	}
}

func TestScannerDoesNotNotifyWhenNoDrift(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()
	scanner := NewScanner(st)
	scanner.Providers = providers.NewRegistry()
	scanner.Providers.Register(expectedResourcesProvider{})
	checker := &fakeNotifier{}
	scanner.Notifiers = []notify.Notifier{checker}
	report, err := scanner.Run(context.Background(), testOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.TotalFindings != 0 || checker.calls != 0 {
		t.Fatalf("expected no findings and no notification, findings=%d calls=%d", report.Summary.TotalFindings, checker.calls)
	}
	if report.EmailNotificationStatus != model.EmailNotificationDisabled {
		t.Fatalf("expected email notification disabled status, got %q", report.EmailNotificationStatus)
	}
}

func TestNotificationFailureDoesNotFailOrLoseScan(t *testing.T) {
	st := openTestStore(t)
	defer st.Close()
	scanner := NewScanner(st)
	checker := &fakeNotifier{err: safeNotificationError("SMTP server smtp.example.test:587: 535 authentication failed")}
	scanner.Notifiers = []notify.Notifier{checker}
	var logs bytes.Buffer
	previousLog := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousLog) })
	report, err := scanner.Run(context.Background(), testOptions(true))
	if err != nil {
		t.Fatalf("notification error failed scan: %v", err)
	}
	if checker.calls != 1 {
		t.Fatalf("expected notification attempt, got %d", checker.calls)
	}
	if report.EmailNotificationStatus != model.EmailNotificationFailed {
		t.Fatalf("expected failed email status, got %q", report.EmailNotificationStatus)
	}
	saved, err := st.GetScan(context.Background(), report.ScanID)
	if err != nil {
		t.Fatalf("scan was not saved: %v", err)
	}
	if saved.Status != model.ScanStatusCompleted {
		t.Fatalf("expected saved completed scan, got %s", saved.Status)
	}
	if saved.EmailNotificationStatus != model.EmailNotificationFailed {
		t.Fatalf("expected persisted failed email status, got %q", saved.EmailNotificationStatus)
	}
	if !strings.Contains(logs.String(), "smtp.example.test:587: 535 authentication failed") {
		t.Fatalf("safe SMTP error was not logged: %s", logs.String())
	}
}

func openTestStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func testOptions(skipCloud bool) Options {
	statePath, _ := filepath.Abs("../../testdata/state/sample.tfstate")
	return Options{WorkspaceName: "test", Provider: "aws", State: model.StateConfig{Backend: "local", Path: statePath}, SkipCloud: skipCloud}
}
