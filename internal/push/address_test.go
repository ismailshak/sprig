package push

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestRefusedAddress_RefusesEveryAddressOnAPrivateNetworkAndNoPublicOne(t *testing.T) {
	cases := []struct {
		addr    string
		refused bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"fd00::1", true},
		{"169.254.169.254", true},
		{"fe80::1", true},
		{"0.0.0.0", true},
		{"::", true},
		{"224.0.0.1", true},
		{"ff02::1", true},
		{"100.64.0.0", true},
		{"100.127.255.255", true},
		{"::ffff:10.0.0.1", true},
		{"::ffff:127.0.0.1", true},
		{"::ffff:100.64.0.1", true},
		{"100.63.255.255", false},
		{"100.128.0.0", false},
		{"142.250.80.42", false},
		{"2607:f8b0:4004:c1b::5f", false},
		{"::ffff:142.250.80.42", false},
	}
	for _, c := range cases {
		if got := RefusedAddress(netip.MustParseAddr(c.addr)); got != c.refused {
			t.Errorf("RefusedAddress(%s) = %t, want %t", c.addr, got, c.refused)
		}
	}
}

func TestSend_WithoutAClientMakesNoRequestToAnEndpointThatNamesALoopbackAddress(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	sender := NewSender(testKeys(t), nil)

	err := sender.Send(t.Context(), browserSubscription(t, service.URL+"/send/abc"), Notification{Title: "Rosewood"})

	if !errors.Is(err, ErrRefusedAddress) {
		t.Errorf("Send returned %v, want ErrRefusedAddress", err)
	}
	if paths := service.received(); len(paths) != 0 {
		t.Errorf("the push service received %v, want nothing", paths)
	}
}

func TestSend_WithoutAClientMakesNoRequestToAnEndpointWhoseNameResolvesToLoopback(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	sender := NewSender(testKeys(t), nil)
	endpoint := strings.Replace(service.URL, "127.0.0.1", "localhost", 1) + "/send/abc"

	err := sender.Send(t.Context(), browserSubscription(t, endpoint), Notification{Title: "Rosewood"})

	if !errors.Is(err, ErrRefusedAddress) {
		t.Errorf("Send returned %v, want ErrRefusedAddress", err)
	}
	if paths := service.received(); len(paths) != 0 {
		t.Errorf("the push service received %v, want nothing", paths)
	}
}

// The client allows 127.0.0.1, the test server's address, because the first
// request has to succeed before there is a redirect to refuse.
func TestSend_ARedirectToARefusedAddressIsNotFollowed(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(service.Close)
	serviceAddr := netip.MustParseAddrPort(service.Listener.Addr().String()).Addr()
	client := guardedClient(func(addr netip.Addr) bool { return addr != serviceAddr && RefusedAddress(addr) })
	sender := NewSender(testKeys(t), client)

	err := sender.Send(t.Context(), browserSubscription(t, service.URL+"/send/abc"), Notification{Title: "Rosewood"})

	if !errors.Is(err, ErrRefusedAddress) {
		t.Errorf("Send returned %v, want ErrRefusedAddress", err)
	}
}
