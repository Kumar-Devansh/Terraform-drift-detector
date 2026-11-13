// Package notify sends finalized scan reports to external notification systems.
package notify

import (
	"context"

	"github.com/driftctl/driftctl/internal/config"
	"github.com/driftctl/driftctl/internal/model"
)

// Notifier is implemented by each notification provider.
type Notifier interface {
	Name() string
	Send(context.Context, *model.DriftReport) error
}

// SafeError marks errors whose SafeMessage is suitable for logs.
// Providers should use it when the underlying error may contain credentials.
type SafeError interface {
	error
	SafeMessage() string
}

// FromConfig builds all enabled notification providers.
func FromConfig(cfg config.NotificationsConfig) []Notifier {
	var notifiers []Notifier
	if cfg.Slack.Enabled {
		notifiers = append(notifiers, NewSlack(cfg.Slack))
	}
	if cfg.Email.Enabled {
		notifiers = append(notifiers, NewEmail(cfg.Email))
	}
	return notifiers
}
