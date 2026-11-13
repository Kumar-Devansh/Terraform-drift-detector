package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/driftctl/driftctl/internal/config"
	"github.com/driftctl/driftctl/internal/model"
)

// Slack sends concise scan summaries to a Slack incoming webhook.
type Slack struct {
	webhookURL string
	client     *http.Client
}

func NewSlack(cfg config.SlackConfig) *Slack {
	return &Slack{
		webhookURL: strings.TrimSpace(os.ExpandEnv(cfg.WebhookURL)),
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Slack) Name() string { return "slack" }

func (s *Slack) Send(ctx context.Context, report *model.DriftReport) error {
	if s.webhookURL == "" {
		return fmt.Errorf("webhook URL is not configured")
	}
	payload := map[string]string{"text": slackMessage(report)}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode notification")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create notification request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		// Avoid returning the underlying error: it may include the secret URL.
		return fmt.Errorf("send notification")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func slackMessage(report *model.DriftReport) string {
	critical, warnings, info := 0, 0, 0
	for _, finding := range report.Findings {
		switch finding.Severity {
		case model.SeverityCritical:
			critical++
		case model.SeverityWarning:
			warnings++
		case model.SeverityInfo:
			info++
		}
	}
	workspace := report.Workspace
	if workspace == "" {
		workspace = "(unnamed)"
	}
	return fmt.Sprintf("🚨 Terraform Drift Detected\n\nWorkspace: %s\nScan ID: %s\n\nCritical: %d\nWarnings: %d\nInfo: %d\n\nTotal Findings: %d",
		workspace, report.ScanID, critical, warnings, info, report.Summary.TotalFindings)
}
