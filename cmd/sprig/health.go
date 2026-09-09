package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// healthTimeout is how long sprig health waits for the response to its GET of
// /healthz.
const healthTimeout = 5 * time.Second

// health is the container's health check. It GETs /healthz on SPRIG_ADDR and
// returns an error for anything but a 200. It reads that one variable rather
// than the whole config, because a missing SPRIG_DATABASE_URL would otherwise
// report a running server as unhealthy.
func health(ctx context.Context, getenv func(string) string) error {
	target, err := healthURL(withDefault(getenv("SPRIG_ADDR"), defaultAddr))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: healthTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", target, resp.Status)
	}
	return nil
}

// healthURL is the URL of /healthz on the server listening at addr. An empty
// or unspecified host, such as 0.0.0.0 or ::, becomes 127.0.0.1, because a
// server listening on every interface is reached over loopback from inside the
// container.
func healthURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("SPRIG_ADDR must be host:port, got %q", addr)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}
