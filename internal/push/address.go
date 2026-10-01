package push

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// sharedAddressSpace is set aside for carrier-grade NAT. VPNs assign addresses
// from it as well. IsPrivate does not include it.
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// ErrRefusedAddress is wrapped in the error Send returns when the client
// refuses to connect to an address RefusedAddress refuses, at the endpoint or
// at a redirect.
var ErrRefusedAddress = errors.New("refused to connect to a private address")

// RefusedAddress reports whether addr is invalid, loopback, private,
// link-local, unspecified, multicast or in 100.64.0.0/10. A member's browser
// posts the push endpoint. Without this check a member could have the server
// send requests to machines on the network it runs on. Every push service is
// on the public internet.
func RefusedAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return !addr.IsValid() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() ||
		sharedAddressSpace.Contains(addr)
}

// guardedClient returns an http.Client with a 10-second timeout whose dialer
// refuses any address refused reports true for. The dialer checks the resolved
// address a connection is opened to, on the first request and on every
// redirect. The transport has no proxy, because through one the dialer would
// check the proxy's address and not the push service's.
func guardedClient(refused func(netip.Addr) bool) *http.Client {
	dialer := &net.Dialer{
		Control: func(_, address string, _ syscall.RawConn) error {
			addrPort, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("reading the address being dialed: %w", err)
			}
			if refused(addrPort.Addr()) {
				return ErrRefusedAddress
			}
			return nil
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	return &http.Client{Timeout: 10 * time.Second, Transport: transport}
}
