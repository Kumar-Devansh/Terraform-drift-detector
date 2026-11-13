package scan

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/driftctl/driftctl/internal/drift"
	"github.com/driftctl/driftctl/internal/model"
	"github.com/driftctl/driftctl/internal/notify"
	"github.com/driftctl/driftctl/internal/providers"
	"github.com/driftctl/driftctl/internal/state"
	"github.com/driftctl/driftctl/internal/store"
)

// Scanner orchestrates drift detection scans.
type Scanner struct {
	StateReader *state.DefaultReader
	Extractor   *state.Extractor
	Providers   *providers.Registry
	Store       store.Store
	Notifiers   []notify.Notifier
}

// Options configures a single scan run.
type Options struct {
	WorkspaceID   string
	WorkspaceName string
	Provider      string
	State         model.StateConfig
	Regions       []string
	ResourceTypes []string
	Compare       model.CompareConfig
	SkipCloud     bool // for testing with state-only
}

func NewScanner(st store.Store) *Scanner {
	return &Scanner{
		StateReader: state.NewReader(),
		Providers:   providers.DefaultRegistry(),
		Store:       st,
	}
}

// Run executes a full drift scan pipeline.
func (s *Scanner) Run(ctx context.Context, opts Options) (*model.DriftReport, error) {
	scanID := uuid.New().String()
	started := time.Now().UTC()

	report := &model.DriftReport{
		ScanID:                  scanID,
		WorkspaceID:             opts.WorkspaceID,
		Workspace:               opts.WorkspaceName,
		StartedAt:               started,
		Status:                  model.ScanStatusRunning,
		EmailNotificationStatus: model.EmailNotificationDisabled,
	}

	if err := s.Store.SaveScan(ctx, report); err != nil {
		return nil, fmt.Errorf("save scan: %w", err)
	}

	extractor := state.NewExtractor(opts.Provider)
	stateData, err := s.StateReader.Read(ctx, opts.State)
	if err != nil {
		return s.failScan(ctx, report, fmt.Errorf("read state: %w", err))
	}

	expected, err := extractor.Extract(stateData)
	if err != nil {
		return s.failScan(ctx, report, fmt.Errorf("extract state: %w", err))
	}
	// Explicit regions are the workspace's scan boundary. Excluding state
	// resources outside it prevents false missing_in_cloud findings.
	if len(opts.Regions) > 0 {
		inScope := make(map[string]bool, len(opts.Regions))
		for _, region := range opts.Regions {
			inScope[region] = true
		}
		filtered := expected[:0]
		for _, resource := range expected {
			if resource.Region == "" || inScope[resource.Region] {
				filtered = append(filtered, resource)
			}
		}
		expected = filtered
	}
	expected = filterResourceTypes(expected, opts.ResourceTypes)

	var actual []model.Resource
	var fetchErrors []string

	if !opts.SkipCloud {
		provider, ok := s.Providers.Get(opts.Provider)
		if !ok {
			return s.failScan(ctx, report, fmt.Errorf("unsupported provider: %s", opts.Provider))
		}
		actual, err = provider.FetchResources(ctx, expected, opts.Regions, opts.ResourceTypes)
		actual = filterResourceTypes(actual, opts.ResourceTypes)
		if err != nil {
			fetchErrors = append(fetchErrors, err.Error())
		}
	}

	engine := drift.NewEngine(opts.Compare)
	findings := engine.Compare(expected, actual)

	report.CompletedAt = time.Now().UTC()
	report.Status = model.ScanStatusCompleted
	report.Summary = drift.BuildSummary(len(expected), findings)
	report.Findings = findings
	report.Errors = fetchErrors

	if err := s.Store.SaveScan(ctx, report); err != nil {
		return nil, fmt.Errorf("save completed scan: %w", err)
	}
	if report.Summary.TotalFindings > 0 {
		for _, notifier := range s.Notifiers {
			if notifier == nil {
				continue
			}
			notificationErr := notifier.Send(ctx, report)
			if notifier.Name() == "email" {
				if notificationErr != nil {
					report.EmailNotificationStatus = model.EmailNotificationFailed
				} else {
					report.EmailNotificationStatus = model.EmailNotificationSent
				}
			}
			if notificationErr != nil {
				var safeErr notify.SafeError
				if errors.As(notificationErr, &safeErr) {
					log.Printf("notification provider %s failed for scan %s: %s", notifier.Name(), report.ScanID, safeErr.SafeMessage())
				} else {
					log.Printf("notification provider %s failed for scan %s", notifier.Name(), report.ScanID)
				}
			}
		}
	}
	if report.EmailNotificationStatus != model.EmailNotificationDisabled {
		if err := s.Store.SaveScan(ctx, report); err != nil {
			log.Printf("email notification status could not be saved for scan %s", report.ScanID)
		}
	}

	return report, nil
}

func filterResourceTypes(resources []model.Resource, resourceTypes []string) []model.Resource {
	if len(resourceTypes) == 0 {
		return resources
	}
	allowed := make(map[string]bool, len(resourceTypes))
	for _, resourceType := range resourceTypes {
		allowed[resourceType] = true
	}
	filtered := make([]model.Resource, 0, len(resources))
	for _, resource := range resources {
		if allowed[resource.Type] {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

func (s *Scanner) failScan(ctx context.Context, report *model.DriftReport, err error) (*model.DriftReport, error) {
	report.CompletedAt = time.Now().UTC()
	report.Status = model.ScanStatusFailed
	report.Errors = append(report.Errors, err.Error())
	_ = s.Store.SaveScan(ctx, report)
	return report, err
}
