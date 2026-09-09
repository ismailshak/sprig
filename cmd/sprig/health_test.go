package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// healthzAt starts a server on loopback that returns status for /healthz, and
// returns its host:port.
func healthzAt(t *testing.T, status int) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return strings.TrimPrefix(server.URL, "http://")
}

func TestHealth_A200FromHealthzOnTheListenAddressPasses(t *testing.T) {
	addr := healthzAt(t, http.StatusOK)
	env := map[string]string{"SPRIG_ADDR": addr}

	if err := subcommand(t.Context(), []string{"health"}, func(k string) string { return env[k] }, nil); err != nil {
		t.Errorf("health = %v, want nil for a server answering 200", err)
	}
}

func TestHealth_AListenAddressWithNoHostIsCheckedOnLoopback(t *testing.T) {
	_, port, err := net.SplitHostPort(healthzAt(t, http.StatusOK))
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"SPRIG_ADDR": ":" + port}

	if err := subcommand(t.Context(), []string{"health"}, func(k string) string { return env[k] }, nil); err != nil {
		t.Errorf("health = %v, want nil: the default SPRIG_ADDR has no host", err)
	}
}

func TestHealth_AStatusOtherThan200Fails(t *testing.T) {
	env := map[string]string{"SPRIG_ADDR": healthzAt(t, http.StatusServiceUnavailable)}

	if err := subcommand(t.Context(), []string{"health"}, func(k string) string { return env[k] }, nil); err == nil {
		t.Error("health passed a server answering 503")
	}
}

func TestHealth_NothingListeningFails(t *testing.T) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("releasing the port: %v", err)
	}
	env := map[string]string{"SPRIG_ADDR": addr}

	if err := subcommand(t.Context(), []string{"health"}, func(k string) string { return env[k] }, nil); err == nil {
		t.Errorf("health passed with nothing listening on %s", addr)
	}
}

func TestHealthURL_AnUnspecifiedHostIsCheckedOnLoopbackAndANamedHostIsKept(t *testing.T) {
	cases := map[string]string{
		":8080":               "http://127.0.0.1:8080/healthz",
		"0.0.0.0:8080":        "http://127.0.0.1:8080/healthz",
		"[::]:8080":           "http://127.0.0.1:8080/healthz",
		"127.0.0.1:9000":      "http://127.0.0.1:9000/healthz",
		"[::1]:8080":          "http://[::1]:8080/healthz",
		"sprig.internal:8080": "http://sprig.internal:8080/healthz",
	}
	for addr, want := range cases {
		got, err := healthURL(addr)
		if err != nil || got != want {
			t.Errorf("healthURL(%q) = %q, %v; want %q", addr, got, err, want)
		}
	}
	if _, err := healthURL("8080"); err == nil {
		t.Error("healthURL accepted an address with no port separator")
	}
}
