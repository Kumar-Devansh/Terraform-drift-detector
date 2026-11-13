package notify

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/driftctl/driftctl/internal/config"
	"github.com/driftctl/driftctl/internal/model"
)

func TestEmailSendsDriftSummary(t *testing.T) {
	email := NewEmail(config.EmailConfig{
		Enabled: true, SMTPHost: "smtp.example.test", SMTPPort: 587,
		Username: "smtp-user", Password: "smtp-password",
		From: "drift@example.test", To: []string{"ops@example.test"},
	})
	email.sendMail = func(_ context.Context, host, addr, username, password, from string, recipients []string, message []byte) error {
		if host != "smtp.example.test" || addr != "smtp.example.test:587" || username != "smtp-user" || password != "smtp-password" {
			t.Fatalf("unexpected SMTP configuration passed to transport")
		}
		if from != "drift@example.test" || len(recipients) != 1 || recipients[0] != "ops@example.test" {
			t.Fatalf("unexpected email addresses: from=%q to=%v", from, recipients)
		}
		for _, expected := range []string{
			"Subject: Terraform Drift Detected - production",
			"Workspace: production", "Scan ID: scan-123", "Scan Status: completed",
			"- Total Resources: 12", "- Total Findings: 3", "- Critical: 1", "- Warning: 1", "- Info: 1",
		} {
			if !strings.Contains(string(message), expected) {
				t.Errorf("email missing %q:\n%s", expected, message)
			}
		}
		return nil
	}
	report := &model.DriftReport{
		ScanID: "scan-123", Workspace: "production", Status: model.ScanStatusCompleted,
		Summary:  model.DriftSummary{TotalResources: 12, TotalFindings: 3},
		Findings: []model.DriftFinding{{Severity: model.SeverityCritical}, {Severity: model.SeverityWarning}, {Severity: model.SeverityInfo}},
	}
	if err := email.Send(context.Background(), report); err != nil {
		t.Fatal(err)
	}
}

func TestEmailSMTPFailureDoesNotExposeCredentials(t *testing.T) {
	const password = "private-smtp-password"
	const username = "smtp-login"
	encodedAuth := base64.StdEncoding.EncodeToString([]byte("\x00" + username + "\x00" + password))
	rootCause := errors.New("server rejected auth for username=" + username + " password=" + password + " auth=" + encodedAuth)
	email := NewEmail(config.EmailConfig{
		SMTPHost: "smtp.example.test", SMTPPort: 587, Username: username, Password: password,
		From: "drift@example.test", To: []string{"ops@example.test"},
	})
	email.sendMail = func(_ context.Context, _, _, _, _pass, _ string, _ []string, _ []byte) error {
		return rootCause
	}
	err := email.Send(context.Background(), &model.DriftReport{})
	if err == nil {
		t.Fatal("expected SMTP failure")
	}
	for _, secret := range []string{username, password, encodedAuth} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("SMTP error exposed credentials: %v", err)
		}
	}
	if !strings.Contains(err.Error(), "smtp.example.test:587") || !strings.Contains(err.Error(), "server rejected auth") {
		t.Fatalf("SMTP error omitted safe debug context: %v", err)
	}
	if !errors.Is(err, rootCause) {
		t.Fatalf("SMTP cause was not preserved: %v", err)
	}
}

func TestEmailExpandsEnvironmentVariables(t *testing.T) {
	t.Setenv("DRIFTCTL_TEST_SMTP_PASSWORD", "expanded-password")
	t.Setenv("DRIFTCTL_TEST_SMTP_USER", "expanded-user")
	email := NewEmail(config.EmailConfig{
		SMTPHost: "smtp.example.test", From: "drift@example.test", To: []string{"ops@example.test"},
		Username: "${DRIFTCTL_TEST_SMTP_USER}", Password: "${DRIFTCTL_TEST_SMTP_PASSWORD}",
	})
	email.sendMail = func(_ context.Context, _, _, username, password, _ string, _ []string, _ []byte) error {
		if username != "expanded-user" || password != "expanded-password" {
			t.Fatalf("environment variables were not expanded")
		}
		return nil
	}
	if err := email.Send(context.Background(), &model.DriftReport{}); err != nil {
		t.Fatal(err)
	}
}

func TestFromConfigEnablesEmailAndSlackIndependently(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  config.NotificationsConfig
		want []string
	}{
		{name: "email only", cfg: config.NotificationsConfig{Email: config.EmailConfig{Enabled: true}}, want: []string{"email"}},
		{name: "slack and email", cfg: config.NotificationsConfig{Slack: config.SlackConfig{Enabled: true}, Email: config.EmailConfig{Enabled: true}}, want: []string{"slack", "email"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := FromConfig(test.cfg)
			if len(got) != len(test.want) {
				t.Fatalf("expected %d notifiers, got %d", len(test.want), len(got))
			}
			for i, notifier := range got {
				if notifier.Name() != test.want[i] {
					t.Errorf("notifier %d: got %q, want %q", i, notifier.Name(), test.want[i])
				}
			}
		})
	}
}
