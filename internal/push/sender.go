package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/ismailshak/sprig/internal/store"
)

// Notification is the JSON body of a push message. The service worker reads
// these fields out of the push event, so a field added here is added there
// too.
type Notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// URL is the page the browser opens when the notification is pressed.
	URL string `json:"url"`
	// Icon is the URL of the image shown beside the title. When it is empty
	// the service worker shows the app's icon.
	Icon string `json:"icon,omitempty"`
	// Tag groups notifications on the device. A notification with the same
	// tag as one already showing replaces it rather than adding a second.
	Tag string `json:"tag,omitempty"`
	// Again is the Remind me again form the notification's action buttons
	// post. It is nil on a notification with no buttons.
	Again *RemindAgain `json:"again,omitempty"`
}

// RemindAgain is the form a digest notification's action buttons post: the
// URL, the field name, and one button per delay. The service worker posts
// Field=Value for the button pressed. The banner on Today posts the same form.
type RemindAgain struct {
	URL    string  `json:"url"`
	Field  string  `json:"field"`
	Delays []Delay `json:"delays"`
}

// Delay is one fixed delay the digest can be sent again after. Value is what
// the form posts, Label is the banner button's text and Action is the
// notification button's text.
type Delay struct {
	Value    string        `json:"value"`
	Label    string        `json:"label"`
	Action   string        `json:"action"`
	Duration time.Duration `json:"-"`
}

// DelayField is the field name of the delay a Remind me again form posts.
const DelayField = "delay"

// Delays is every fixed delay the Remind me again form offers, in the order
// the buttons appear.
var Delays = []Delay{
	{Value: "1h", Label: "In 1 hour", Action: "Remind me again in 1 hour", Duration: time.Hour},
	{Value: "2h", Label: "In 2 hours", Action: "Remind me again in 2 hours", Duration: 2 * time.Hour},
}

// DelayFor returns the delay whose Value is value, and false when the form
// does not offer it.
func DelayFor(value string) (Delay, bool) {
	for _, delay := range Delays {
		if delay.Value == value {
			return delay, true
		}
	}
	return Delay{}, false
}

// ErrGone is returned by Send when the push service responds 404 or 410,
// meaning the browser unsubscribed or the app was uninstalled. The caller
// deletes the subscription row.
var ErrGone = errors.New("the push service no longer has the subscription")

// ttl is how long, in seconds, the push service holds a message for a device
// that is offline. A day covers a phone that is off overnight, and a digest is
// out of date after that.
const ttl = 24 * 60 * 60

// Sender signs and encrypts push messages with one key pair.
type Sender struct {
	keys   Keys
	client *http.Client
}

// NewSender returns a Sender that signs with keys. A nil client means a new
// http.Client with a 10-second timeout.
func NewSender(keys Keys, client *http.Client) *Sender {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Sender{keys: keys, client: client}
}

// serviceOf returns the scheme and host of a push endpoint, or "the push
// service" when it has no host. It leaves out the path, because anyone holding
// the whole endpoint URL can push to that browser.
func serviceOf(endpoint string) string {
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return "the push service"
}

// sendError wraps err with the scheme and host of the endpoint. It unwraps a
// *url.Error first, because that error's message holds the whole URL.
func sendError(endpoint string, err error) error {
	var transport *url.Error
	if errors.As(err, &transport) {
		err = transport.Err
	}
	return fmt.Errorf("sending to %s: %w", serviceOf(endpoint), err)
}

// Send delivers n to one subscription. It returns ErrGone when the push
// service no longer has the subscription, and another error when the service
// refused the message or could not be reached.
func (s *Sender) Send(ctx context.Context, subscription store.PushSubscription, n Notification) error {
	body, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("encoding the notification: %w", err)
	}
	response, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     webpush.Keys{P256dh: subscription.P256dhKey, Auth: subscription.AuthKey},
	}, &webpush.Options{
		HTTPClient: s.client,
		// The library adds mailto: itself to anything that is not an https
		// URL, so the configured prefix comes off here.
		Subscriber:      strings.TrimPrefix(s.keys.Subject, "mailto:"),
		TTL:             ttl,
		VAPIDPublicKey:  s.keys.Public,
		VAPIDPrivateKey: s.keys.Private,
	})
	if err != nil {
		return sendError(subscription.Endpoint, err)
	}
	// The body is drained and closed so the connection can be reused. The
	// status alone says whether the send failed.
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()

	switch {
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
		return ErrGone
	case response.StatusCode < 200 || response.StatusCode > 299:
		return fmt.Errorf("the push service answered %d", response.StatusCode)
	}
	return nil
}
