package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/driftctl/driftctl/internal/config"
	"github.com/driftctl/driftctl/internal/model"
)

// Email sends drift summaries through an SMTP server.
type Email struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       []string
	sendMail func(context.Context, string, string, string, string, string, []string, []byte) error
}

func NewEmail(cfg config.EmailConfig) *Email {
	port := cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	recipients := make([]string, 0, len(cfg.To))
	for _, recipient := range cfg.To {
		recipients = append(recipients, strings.TrimSpace(os.ExpandEnv(recipient)))
	}
	return &Email{
		host: strings.TrimSpace(os.ExpandEnv(cfg.SMTPHost)), port: port,
		username: os.ExpandEnv(cfg.Username), password: os.ExpandEnv(cfg.Password),
		from: strings.TrimSpace(os.ExpandEnv(cfg.From)), to: recipients,
		sendMail: sendSMTPMail,
	}
}

func (e *Email) Name() string { return "email" }

func (e *Email) Send(ctx context.Context, report *model.DriftReport) error {
	if e.host == "" || e.port < 1 || e.port > 65535 {
		return errors.New("SMTP host or port is not configured")
	}
	from, err := parsedAddress(e.from)
	if err != nil {
		return errors.New("email sender address is invalid")
	}
	if len(e.to) == 0 {
		return errors.New("email recipients are not configured")
	}
	recipients := make([]string, 0, len(e.to))
	for _, value := range e.to {
		address, err := parsedAddress(value)
		if err != nil {
			return errors.New("email recipient address is invalid")
		}
		recipients = append(recipients, address)
	}
	if (e.username == "") != (e.password == "") {
		return errors.New("both SMTP username and password are required")
	}

	message := emailMessage(report, from, recipients)
	addr := net.JoinHostPort(e.host, strconv.Itoa(e.port))
	if err := e.sendMail(ctx, e.host, addr, e.username, e.password, from, recipients, message); err != nil {
		return &smtpDeliveryError{
			host: e.host, port: e.port, username: e.username, password: e.password, cause: err,
		}
	}
	return nil
}

func parsedAddress(value string) (string, error) {
	parsed, err := mail.ParseAddress(value)
	if err != nil || strings.TrimSpace(parsed.Address) == "" {
		return "", errors.New("invalid email address")
	}
	return parsed.Address, nil
}

func emailMessage(report *model.DriftReport, from string, recipients []string) []byte {
	critical, warning, info := 0, 0, 0
	for _, finding := range report.Findings {
		switch finding.Severity {
		case model.SeverityCritical:
			critical++
		case model.SeverityWarning:
			warning++
		case model.SeverityInfo:
			info++
		}
	}
	workspace := safeHeaderValue(report.Workspace)
	if workspace == "" {
		workspace = "(unnamed)"
	}
	toHeaders := make([]string, len(recipients))
	for i, recipient := range recipients {
		toHeaders[i] = (&mail.Address{Address: recipient}).String()
	}
	subject := safeHeaderValue("Terraform Drift Detected - " + workspace)
	body := fmt.Sprintf("Terraform Drift Detected\n\nWorkspace: %s\nScan ID: %s\nScan Status: %s\n\nSummary:\n\n- Total Resources: %d\n- Total Findings: %d\n- Critical: %d\n- Warning: %d\n- Info: %d\n",
		workspace, safeHeaderValue(report.ScanID), safeHeaderValue(report.Status), report.Summary.TotalResources,
		report.Summary.TotalFindings, critical, warning, info)
	body = strings.ReplaceAll(body, "\n", "\r\n")
	return []byte("From: " + (&mail.Address{Address: from}).String() + "\r\n" +
		"To: " + strings.Join(toHeaders, ", ") + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body)
}

func safeHeaderValue(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}

func sendSMTPMail(ctx context.Context, host, addr, username, password, from string, recipients []string, message []byte) error {
	return sendSMTP(ctx, host, addr, username, password, from, recipients, message, 10*time.Second)
}

func sendSMTP(ctx context.Context, host, addr, username, password, from string, recipients []string, message []byte, timeout time.Duration) error {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial SMTP server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stopCancel()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer client.Close()
	secure := false
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
		secure = true
	}

	// Authentication is never sent over an unencrypted connection.
	if username != "" {
		if !secure {
			return errors.New("SMTP authentication requires TLS")
		}
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return fmt.Errorf("authenticate with SMTP server: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("set SMTP recipient: %w", err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("start SMTP message: %w", err)
	}
	if _, err := io.WriteString(w, string(message)); err != nil {
		_ = w.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("close SMTP session: %w", err)
	}
	return nil
}

type smtpDeliveryError struct {
	host     string
	port     int
	username string
	password string
	cause    error
}

func (e *smtpDeliveryError) Error() string { return e.SafeMessage() }
func (e *smtpDeliveryError) Unwrap() error { return e.cause }
func (e *smtpDeliveryError) SafeMessage() string {
	return fmt.Sprintf("SMTP server %s:%d: %s", e.host, e.port, redactSMTPCredentials(e.cause.Error(), e.username, e.password))
}

func redactSMTPCredentials(message, username, password string) string {
	pairs := make([]string, 0, 10)
	secrets := make([]string, 0, 5)
	if username != "" {
		secrets = append(secrets, username, base64.StdEncoding.EncodeToString([]byte(username)))
	}
	if password != "" {
		secrets = append(secrets, password, base64.StdEncoding.EncodeToString([]byte(password)))
	}
	if username != "" && password != "" {
		secrets = append(secrets,
			base64.StdEncoding.EncodeToString([]byte("\x00"+username+"\x00"+password)),
			base64.StdEncoding.EncodeToString([]byte(username+"\x00"+password)),
		)
	}
	for _, secret := range secrets {
		if secret != "" {
			pairs = append(pairs, secret, "[REDACTED]")
		}
	}
	return strings.NewReplacer(pairs...).Replace(message)
}
