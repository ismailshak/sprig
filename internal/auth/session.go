package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

// ErrNoSession reports a token that resolves to no live session. A token never
// issued, one signed out, one revoked and one unused past the TTL share it,
// because a caller treats each the same and a distinct error would tell a
// stranger which tokens were once real.
var ErrNoSession = errors.New("no live session")

// sessionTokenBytes gives a token 256 bits of randomness.
const sessionTokenBytes = 32

// NewSessionToken returns a fresh token for the cookie, encoded as base64url so
// it is a valid cookie value.
func NewSessionToken() string {
	raw := make([]byte, sessionTokenBytes)
	// rand.Read cannot fail since Go 1.24, and stops the process instead.
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// HashToken returns the hex SHA-256 of token, which is what a table stores in
// place of a secret shown once.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SessionDeadline returns the instant a session last used at lastSeen stops
// working. The deadline counts from the last request rather than from sign-in,
// and no absolute cap sits on top of it, because a stolen cookie is in use and
// slides too, so the only session a cap would end is a legitimate one.
func SessionDeadline(lastSeen time.Time, ttl time.Duration) time.Time {
	return lastSeen.Add(ttl)
}

// SessionExpired reports whether a session last used at lastSeen is past its
// deadline at now. The deadline itself counts as expired, which is the instant
// a browser drops a cookie whose Max-Age has run out.
func SessionExpired(lastSeen, now time.Time, ttl time.Duration) bool {
	return !now.Before(SessionDeadline(lastSeen, ttl))
}

// CookieSettings is what a deployment chooses about the session cookie. A
// __Host- name is valid only on a Secure cookie, so a browser that refuses
// Secure over plain http needs both changed at once.
type CookieSettings struct {
	Name   string
	Secure bool
}

// Validate refuses a combination a browser would silently drop.
func (c CookieSettings) Validate() error {
	if c.Name == "" {
		return errors.New("the cookie name is empty")
	}
	if err := (&http.Cookie{Name: c.Name, Value: "x"}).Valid(); err != nil {
		return err
	}
	for _, prefix := range []string{"__Host-", "__Secure-"} {
		if strings.HasPrefix(c.Name, prefix) && !c.Secure {
			return fmt.Errorf("a cookie named %s is refused by browsers unless it is Secure", c.Name)
		}
	}
	return nil
}

// Sessions starts, resolves and ends sessions against the session table, and
// builds the cookie that carries one.
type Sessions struct {
	queries *store.Queries
	ttl     time.Duration
	cookie  CookieSettings
}

// NewSessions returns Sessions over queries with a session surviving ttl of
// disuse. It truncates ttl to whole seconds, because Max-Age is an integer and
// the cookie and the row have to name the same deadline.
func NewSessions(queries *store.Queries, ttl time.Duration, cookie CookieSettings) *Sessions {
	return &Sessions{queries: queries, ttl: ttl.Truncate(time.Second), cookie: cookie}
}

// Create starts a session for userID on gardenID at now and returns the token
// to put in the cookie. userAgent may be empty. The row's foreign key is the
// membership, so a user who is not in the garden cannot be given a session
// on it.
func (s *Sessions) Create(ctx context.Context, now time.Time, userID, gardenID uuid.UUID, userAgent string) (string, store.Session, error) {
	token := NewSessionToken()
	params := store.CreateSessionParams{
		TokenHash: HashToken(token),
		UserID:    userID,
		GardenID:  gardenID,
		Now:       now,
	}
	if userAgent != "" {
		params.UserAgent = &userAgent
	}
	session, err := s.queries.CreateSession(ctx, params)
	if err != nil {
		return "", store.Session{}, fmt.Errorf("create the session: %w", err)
	}
	return token, session, nil
}

// Lookup resolves the token a request presented at now, sliding the window
// forward when it is live. An expired row is deleted here rather than by a
// sweep, since the request that presents it is the only thing that will ever
// touch it again.
func (s *Sessions) Lookup(ctx context.Context, now time.Time, token string) (store.Session, error) {
	hash := HashToken(token)

	session, err := s.queries.GetSessionByTokenHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Session{}, ErrNoSession
	}
	if err != nil {
		return store.Session{}, fmt.Errorf("read the session: %w", err)
	}

	if SessionExpired(session.LastSeenAt, now, s.ttl) {
		if err := s.queries.DeleteSession(ctx, hash); err != nil {
			return store.Session{}, fmt.Errorf("delete the expired session: %w", err)
		}
		return store.Session{}, ErrNoSession
	}

	// A clock reading earlier than the row must not move the window back.
	if !now.After(session.LastSeenAt) {
		return session, nil
	}
	session, err = s.queries.TouchSession(ctx, now, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		// The row went between the read and the touch, through a sign-out
		// elsewhere or a deleted membership.
		return store.Session{}, ErrNoSession
	}
	if err != nil {
		return store.Session{}, fmt.Errorf("touch the session: %w", err)
	}
	return session, nil
}

// Delete ends the session behind token, so a cookie still carrying it resolves
// to nothing. Sign out calls this rather than only clearing the cookie, because
// a cleared cookie ends the copy in one browser.
func (s *Sessions) Delete(ctx context.Context, token string) error {
	if err := s.queries.DeleteSession(ctx, HashToken(token)); err != nil {
		return fmt.Errorf("delete the session: %w", err)
	}
	return nil
}

// Cookie returns the cookie carrying token. HttpOnly keeps it from script,
// SameSite=Lax withholds it from a cross-site form post, and Path=/ is what
// __Host- requires. Max-Age is the TTL, so a browser closed for longer does not
// send a cookie the server would refuse. A caller issues it at sign-in and
// again on every request that slides the window, so the browser's deadline
// follows the row's.
func (s *Sessions) Cookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     s.cookie.Name,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.ttl / time.Second),
		HttpOnly: true,
		Secure:   s.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearedCookie returns the cookie that removes the session cookie from the
// browser. It carries the attributes of Cookie, because a browser replaces only
// a cookie whose name, path and host prefix match.
func (s *Sessions) ClearedCookie() *http.Cookie {
	cookie := s.Cookie("")
	cookie.MaxAge = -1
	return cookie
}

// TokenFromRequest returns the token the request's session cookie carries, or
// the empty string when there is none.
func (s *Sessions) TokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(s.cookie.Name)
	if err != nil {
		return ""
	}
	return cookie.Value
}
