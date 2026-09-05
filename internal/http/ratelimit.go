package http

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/ismailshak/sprig/internal/auth"
)

// Limiter holds one token bucket per key. It deletes a bucket nobody has used
// for a refill, so a flood of distinct keys cannot grow the map without bound.
type Limiter struct {
	limit rate.Limit
	burst int
	// refill is how long an empty bucket takes to fill. It is also how often
	// the map is swept and how long a bucket sits idle before it is deleted.
	refill time.Duration

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewLimiter returns a Limiter whose buckets refill at limit tokens a second
// and hold at most burst. It panics rather than returning an error, because
// both values are constants the caller writes.
func NewLimiter(limit rate.Limit, burst int) *Limiter {
	if limit <= 0 || burst < 1 {
		panic("NewLimiter: the rate must be positive and the burst at least one")
	}
	return &Limiter{
		limit:   limit,
		burst:   burst,
		refill:  time.Duration(float64(burst) / float64(limit) * float64(time.Second)),
		buckets: map[string]*bucket{},
	}
}

// Allow spends one token from key's bucket at now. An empty bucket reports
// false and the time until it next holds a token. A refused attempt spends
// nothing.
func (l *Limiter) Allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) >= l.refill {
		l.sweep(now)
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now

	reservation := b.limiter.ReserveN(now, 1)
	if delay := reservation.DelayFrom(now); delay > 0 {
		reservation.CancelAt(now)
		return false, delay
	}
	return true, 0
}

// sweep deletes buckets idle for a refill. Such a bucket is full again and
// indistinguishable from one that does not exist, so deleting it changes no
// later result.
func (l *Limiter) sweep(now time.Time) {
	l.lastSweep = now
	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) >= l.refill {
			delete(l.buckets, key)
		}
	}
}

// Limit refuses a request whose key has spent its budget with a 429 and a
// Retry-After. A route with both a per-source budget and a shared one wraps the
// shared limiter inside the per-source limiter, so a source already refused
// spends nothing from the shared budget.
func Limit(l *Limiter, key func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retryAfter := l.Allow(key(r), time.Now())
			if !ok {
				// Retry-After is whole seconds, and zero would invite a
				// retry the bucket refuses again.
				seconds := max(int(math.Ceil(retryAfter.Seconds())), 1)
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
				http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientAddress returns a key function for a per-address limiter: the value of
// header when the request has one, otherwise the host part of RemoteAddr. The
// value is used as sent. A client can set header itself, so the operator names
// one only where every request reaches sprig through the proxy that sets it,
// and it has to be a header the proxy overwrites, such as CF-Connecting-IP.
// X-Forwarded-For is appended to rather than replaced, so a client could choose
// its own key.
func ClientAddress(header string) func(*http.Request) string {
	return func(r *http.Request) string {
		if header != "" {
			if v := strings.TrimSpace(r.Header.Get(header)); v != "" {
				return v
			}
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return host
	}
}

// AnySource keys every request alike, for a budget the whole route shares.
func AnySource(*http.Request) string {
	return ""
}

// ByToken keys a request on the hash of the bearer token it presents, so two
// tokens from one address get a bucket each. Every request presenting no token
// shares one bucket.
func ByToken(r *http.Request) string {
	token := bearerToken(r)
	if token == "" {
		return ""
	}
	return auth.HashToken(token)
}

func bearerToken(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
