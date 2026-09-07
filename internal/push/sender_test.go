package push

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

// pushService is a test HTTP server in place of a browser vendor's push
// service. It replies to every message with status, or with the status statuses
// holds for the request's path. It records the last request, the path of every
// message and when the first one arrived.
type pushService struct {
	*httptest.Server
	status   int
	statuses map[string]int
	request  *http.Request
	body     []byte

	mu    sync.Mutex
	paths []string
	first time.Time
}

func newPushService(t *testing.T, status int) *pushService {
	t.Helper()

	service := &pushService{status: status, statuses: map[string]int{}}
	service.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.mu.Lock()
		defer service.mu.Unlock()
		service.request = r
		service.body, _ = io.ReadAll(r.Body)
		service.paths = append(service.paths, r.URL.Path)
		if service.first.IsZero() {
			service.first = time.Now()
		}
		if status, ok := service.statuses[r.URL.Path]; ok {
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(service.status)
	}))
	t.Cleanup(service.Close)
	return service
}

// firstReceived returns the time the first message arrived, and false when
// none has.
func (s *pushService) firstReceived() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.first, !s.first.IsZero()
}

// received returns the path of every message so far, sorted.
func (s *pushService) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths := slices.Clone(s.paths)
	slices.Sort(paths)
	return paths
}

// browserSubscription returns a subscription with the keys a browser would
// make: a P-256 public key and a 16-byte secret, both base64url.
func browserSubscription(t *testing.T, endpoint string) store.PushSubscription {
	t.Helper()

	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating the browser's key: %v", err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("generating the browser's secret: %v", err)
	}
	return store.PushSubscription{
		Endpoint:  endpoint,
		P256dhKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		AuthKey:   base64.RawURLEncoding.EncodeToString(secret),
	}
}

func testKeys(t *testing.T) Keys {
	t.Helper()

	keys, err := GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}
	keys.Subject = "mailto:sprig@example.com"
	return keys
}

func TestSend_PostsASignedEncryptedMessageToTheSubscriptionsEndpoint(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	keys := testKeys(t)
	sender := NewSender(keys, service.Client())
	notification := Notification{Title: "Rosewood", Body: "3 plants need water", URL: "https://sprig.example.com/"}

	err := sender.Send(t.Context(), browserSubscription(t, service.URL+"/send/abc"), notification)

	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if service.request.URL.Path != "/send/abc" {
		t.Errorf("the message went to %s, want /send/abc", service.request.URL.Path)
	}
	authorization := service.request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "vapid t=") || !strings.Contains(authorization, "k="+keys.Public) {
		t.Errorf("Authorization = %q, want a VAPID token signed under the configured public key", authorization)
	}
	if got := service.request.Header.Get("Content-Encoding"); got != "aes128gcm" {
		t.Errorf("Content-Encoding = %q, want aes128gcm", got)
	}
	if got := service.request.Header.Get("TTL"); got != "86400" {
		t.Errorf("TTL = %q, want 86400", got)
	}
	if len(service.body) == 0 || strings.Contains(string(service.body), notification.Body) {
		t.Errorf("the body is %q, want the notification encrypted", service.body)
	}
}

func TestSend_ReturnsErrGoneWhenThePushServiceHasDroppedTheSubscription(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		service := newPushService(t, status)
		sender := NewSender(testKeys(t), service.Client())

		err := sender.Send(t.Context(), browserSubscription(t, service.URL), Notification{Title: "Rosewood"})

		if !errors.Is(err, ErrGone) {
			t.Errorf("a %d from the push service returned %v, want ErrGone", status, err)
		}
	}
}

func TestSend_AnUnreachablePushServiceIsNamedWithoutTheEndpointsPath(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	endpoint := service.URL + "/send/only-this-browser-has-this"
	service.Close()
	sender := NewSender(testKeys(t), service.Client())

	err := sender.Send(t.Context(), browserSubscription(t, endpoint), Notification{Title: "Rosewood"})

	if err == nil {
		t.Fatal("an unreachable push service returned no error")
	}
	if strings.Contains(err.Error(), "only-this-browser-has-this") {
		t.Errorf("the error names the endpoint anyone could push to: %v", err)
	}
	if !strings.Contains(err.Error(), service.URL) {
		t.Errorf("the error is %v, want it to name %s", err, service.URL)
	}
}

func TestSend_ReturnsTheStatusWhenThePushServiceRefuses(t *testing.T) {
	service := newPushService(t, http.StatusBadGateway)
	sender := NewSender(testKeys(t), service.Client())

	err := sender.Send(t.Context(), browserSubscription(t, service.URL), Notification{Title: "Rosewood"})

	if err == nil || errors.Is(err, ErrGone) || !strings.Contains(err.Error(), "502") {
		t.Errorf("a 502 from the push service returned %v, want an error naming the status", err)
	}
}
