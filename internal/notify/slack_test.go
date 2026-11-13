package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/driftctl/driftctl/internal/config"
	"github.com/driftctl/driftctl/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestSlackSendsDriftSummary(t *testing.T) {
	var message map[string]string
	sender := NewSlack(config.SlackConfig{Enabled: true, WebhookURL: "https://hooks.slack.test/webhook"})
	sender.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})}
	report := &model.DriftReport{ScanID: "scan-123", Workspace: "production", Summary: model.DriftSummary{TotalFindings: 3}, Findings: []model.DriftFinding{
		{Severity: model.SeverityCritical}, {Severity: model.SeverityWarning}, {Severity: model.SeverityInfo},
	}}
	if err := sender.Send(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"production", "scan-123", "Critical: 1", "Warnings: 1", "Info: 1", "Total Findings: 3"} {
		if !strings.Contains(message["text"], expected) {
			t.Errorf("message missing %q: %s", expected, message["text"])
		}
	}
}

func TestSlackDoesNotReturnWebhookURLInRequestError(t *testing.T) {
	secretURL := "http://user:super-secret@127.0.0.1:1/webhook"
	sender := NewSlack(config.SlackConfig{WebhookURL: secretURL})
	err := sender.Send(context.Background(), &model.DriftReport{})
	if err == nil {
		t.Fatal("expected request failure")
	}
	if strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), secretURL) {
		t.Fatalf("error exposed webhook URL: %v", err)
	}
}
