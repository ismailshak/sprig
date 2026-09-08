package push

import (
	"context"
	"strings"
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

// TestMessage sends one notification to one browser. The Notifications page's
// Send a test button uses it.
type TestMessage struct {
	queries *store.Queries
	sender  *Sender
	// baseURL is the origin the app is served at, with no trailing slash.
	baseURL string
	// now supplies the value written to last_sent_at.
	now func() time.Time
}

// NewTestMessage returns a TestMessage that sends through sender. A trailing
// slash on baseURL is trimmed, because the paths put after it start with one.
func NewTestMessage(queries *store.Queries, sender *Sender, baseURL string) *TestMessage {
	return &TestMessage{
		queries: queries,
		sender:  sender,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		now:     time.Now,
	}
}

// Send delivers n to subscription and records the send on its row. n.URL and
// n.Icon are paths and are made absolute under the base URL. A subscription
// the push service no longer has is deleted and ErrGone returned.
func (t *TestMessage) Send(ctx context.Context, subscription store.PushSubscription, n Notification) error {
	n.URL = absolute(t.baseURL, n.URL)
	n.Icon = absolute(t.baseURL, n.Icon)
	return sendOne(ctx, t.queries, t.sender, subscription, n, t.now())
}
