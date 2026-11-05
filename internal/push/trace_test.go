package push

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

// recordSpans returns a provider whose spans are kept in the returned recorder
// once they end.
func recordSpans() (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	recorder := tracetest.NewSpanRecorder()
	return sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)), recorder
}

func TestSend_TheSpanRecordsThePushServiceAndStatusAndNotTheEndpointPath(t *testing.T) {
	const path = "/wpush/v2/gAAAAABpZXJzb25hbC1lbmRwb2ludA"
	provider, recorder := recordSpans()
	service := newPushService(t, http.StatusCreated)
	sender := NewSender(testKeys(t), service.Client(), provider)

	if err := sender.Send(t.Context(), browserSubscription(t, service.URL+path), Notification{Title: "Rosewood"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "push send" {
		t.Fatalf("got %d spans, want one named push send", len(spans))
	}
	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.String()
		if strings.Contains(kv.Value.String(), path) {
			t.Errorf("%s = %q holds the endpoint's path", kv.Key, kv.Value.String())
		}
	}
	if attrs["server.address"] != service.URL {
		t.Errorf("server.address = %q, want %q", attrs["server.address"], service.URL)
	}
	if attrs["http.response.status_code"] != "201" {
		t.Errorf("http.response.status_code = %q, want 201", attrs["http.response.status_code"])
	}
}

func TestSend_ARefusedMessageSetsTheSpanStatusToError(t *testing.T) {
	provider, recorder := recordSpans()
	service := newPushService(t, http.StatusBadRequest)
	sender := NewSender(testKeys(t), service.Client(), provider)

	if err := sender.Send(t.Context(), browserSubscription(t, service.URL+"/send/abc"), Notification{Title: "Rosewood"}); err == nil {
		t.Fatal("Send returned nil for a 400")
	}

	if got := recorder.Ended()[0].Status().Code; got != codes.Error {
		t.Errorf("span status = %v, want Error", got)
	}
}

// runOneLook runs runJob with look until look has been called once, and
// returns the spans recorded.
func runOneLook(t *testing.T, look func(context.Context) (time.Time, error)) []sdktrace.ReadOnlySpan {
	t.Helper()
	provider, recorder := recordSpans()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	once := func(ctx context.Context) (time.Time, error) {
		defer cancel()
		return look(ctx)
	}
	runJob(ctx, slog.New(slog.DiscardHandler), provider.Tracer("test"), "digest", time.Now, make(chan struct{}), once)
	return recorder.Ended()
}

func TestRunJob_OneLookIsOneRootSpanNamedAfterTheJob(t *testing.T) {
	spans := runOneLook(t, func(context.Context) (time.Time, error) { return time.Time{}, nil })

	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Name() != "digest" {
		t.Errorf("name = %q, want digest", spans[0].Name())
	}
	if spans[0].Parent().IsValid() {
		t.Error("the look's span has a parent, want a root span")
	}
	if spans[0].Status().Code == codes.Error {
		t.Error("a look that succeeded set the span's status to Error")
	}
}

func TestRunJob_AFailedLookSetsTheSpanStatusToError(t *testing.T) {
	spans := runOneLook(t, func(context.Context) (time.Time, error) { return time.Time{}, errors.New("connection refused") })

	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", spans[0].Status().Code)
	}
}

func TestNotifier_AnActivitySendIsInTheTraceOfTheRequestThatLoggedTheCare(t *testing.T) {
	provider, recorder := recordSpans()
	service := newPushService(t, http.StatusCreated)
	tx := pgtest.Tx(t, migrateSchema)
	seedRosewood(t, tx, service)
	turnActivityOn(t, tx, samMembershipID)
	notifier := NewNotifier(slog.New(slog.DiscardHandler), store.New(tx), NewSender(testKeys(t), service.Client(), provider), "https://sprig.example.com/")

	ctx, request := provider.Tracer("test").Start(t.Context(), "POST /plants/{plant}/log")
	notifier.SendActivity(ctx, rosewoodID, ellieID, wateredDoris)
	request.End()
	notifier.Wait()

	spans := map[string]sdktrace.ReadOnlySpan{}
	for _, span := range recorder.Ended() {
		spans[span.Name()] = span
	}
	notify, send := spans["notify activity"], spans["push send"]
	if notify == nil || send == nil {
		t.Fatalf("got spans %q, want notify activity and push send", slices.Collect(maps.Keys(spans)))
	}
	if notify.Parent().SpanID() != request.SpanContext().SpanID() {
		t.Error("notify activity is not a child of the request's span")
	}
	if send.Parent().SpanID() != notify.SpanContext().SpanID() {
		t.Error("push send is not a child of notify activity")
	}
}
