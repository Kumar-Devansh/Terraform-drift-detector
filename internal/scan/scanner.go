package scan

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/driftctl/driftctl/internal/drift"
	"github.com/driftctl/driftctl/internal/model"
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
}

// Options configures a single scan run.
type Options struct {
	WorkspaceID   string
	WorkspaceName string
	Provider      string
	State         model.StateConfig
	Regions       []string
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
		ScanID:      scanID,
		WorkspaceID: opts.WorkspaceID,
		Workspace:   opts.WorkspaceName,
		StartedAt:   started,
		Status:      model.ScanStatusRunning,
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

	var actual []model.Resource
	var fetchErrors []string

	if !opts.SkipCloud {
		provider, ok := s.Providers.Get(opts.Provider)
		if !ok {
			return s.failScan(ctx, report, fmt.Errorf("unsupported provider: %s", opts.Provider))
		}
		actual, err = provider.FetchResources(ctx, expected, opts.Regions)
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

	return report, nil
}

func (s *Scanner) failScan(ctx context.Context, report *model.DriftReport, err error) (*model.DriftReport, error) {
	report.CompletedAt = time.Now().UTC()
	report.Status = model.ScanStatusFailed
	report.Errors = append(report.Errors, err.Error())
	_ = s.Store.SaveScan(ctx, report)
	return report, err
}
