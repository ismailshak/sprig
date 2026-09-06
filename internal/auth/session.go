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

// ErrNoSession is returned for a token with no live session. A token that was
// never issued, one signed out, one revoked and one unused past the TTL all
// return the same error, because callers treat them the same and a distinct
// error would tell an attacker which tokens were once real.
var ErrNoSession = errors.New("no live session")

// sessionTokenBytes is 256 bits of randomness per token.
const sessionTokenBytes = 32

// NewSessionToken returns a fresh token for the cookie, encoded as base64url so
// it is a valid cookie value.
func NewSessionToken() string {
	raw := make([]byte, sessionTokenBytes)
	// rand.Read cannot fail since Go 1.24, and stops the process instead.
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// HashToken returns the hex SHA-256 of token. Tables store the hash, never the
// token itself.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SessionDeadline returns the instant a session last used at lastSeen stops
// working. The deadline counts from the last request rather than from sign-in,
// with no absolute cap, because a stolen cookie is in use and would slide too,
// so the only session a cap would end is a legitimate one.
func SessionDeadline(lastSeen time.Time, ttl time.Duration) time.Time {
	return lastSeen.Add(ttl)
}

// SessionExpired reports whether a session last used at lastSeen has passed
// its deadline at now. The deadline instant itself counts as expired, matching
// when a browser drops a cookie whose Max-Age has run out.
func SessionExpired(lastSeen, now time.Time, ttl time.Duration) bool {
	return !now.Before(SessionDeadline(lastSeen, ttl))
}

// CookieSettings is the session cookie's name and Secure flag, set per
// deployment. A __Host- name is only valid on a Secure cookie, so a local
// deployment over plain http has to change both.
type CookieSettings struct {
	Name   string
	Secure bool
}

// Validate returns an error for a name and Secure combination a browser would
// silently drop.
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

// Sessions creates, looks up and deletes rows in the session table, and builds
// the cookie that holds a session token.
type Sessions struct {
	queries *store.Queries
	ttl     time.Duration
	cookie  CookieSettings
}

// NewSessions returns Sessions whose sessions expire after ttl without use.
// ttl is truncated to whole seconds, because Max-Age is an integer and the
// cookie and the row have to agree on the deadline.
func NewSessions(queries *store.Queries, ttl time.Duration, cookie CookieSettings) *Sessions {
	return &Sessions{queries: queries, ttl: ttl.Truncate(time.Second), cookie: cookie}
}

// Create starts a session for userID on gardenID at now and returns the token
// to put in the cookie. passkeyID is the passkey the sign-in used. Removing
// that passkey deletes the session, so the device it was on is signed out then
// rather than when the session expires. Only the development sign-in passes
// nil, because it uses no passkey. userAgent may be empty. The row's foreign
// key points at the membership, so a user who is not in the garden cannot get
// a session on it.
func (s *Sessions) Create(ctx context.Context, now time.Time, userID, gardenID uuid.UUID, passkeyID *uuid.UUID, userAgent string) (string, store.Session, error) {
	token := NewSessionToken()
	params := store.CreateSessionParams{
		TokenHash:           HashToken(token),
		UserID:              userID,
		GardenID:            gardenID,
		PasskeyCredentialID: passkeyID,
		Now:                 now,
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

// Lookup returns the session for token and moves its deadline forward to now
// plus the TTL. An expired row is deleted here rather than by a sweep, since
// the request presenting it is the only thing that will ever touch it again.
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

	// A clock reading earlier than the row must not move the deadline back.
	if !now.After(session.LastSeenAt) {
		return session, nil
	}
	session, err = s.queries.TouchSession(ctx, now, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		// The row was deleted between the read and the update, by a sign-out
		// elsewhere or a deleted membership.
		return store.Session{}, ErrNoSession
	}
	if err != nil {
		return store.Session{}, fmt.Errorf("touch the session: %w", err)
	}
	return session, nil
}

// Delete removes the session for token, so a cookie still holding it no longer
// resolves. Sign out calls this as well as clearing the cookie, because clearing
// the cookie only ends the copy in one browser.
func (s *Sessions) Delete(ctx context.Context, token string) error {
	if err := s.queries.DeleteSession(ctx, HashToken(token)); err != nil {
		return fmt.Errorf("delete the session: %w", err)
	}
	return nil
}

// DeleteFromRequest removes the session for the token in the request's session
// cookie. A request with no session cookie deletes nothing and is not an error.
// A handler that starts a new session calls this first, so the token the
// browser arrived with stops resolving rather than staying live until its TTL.
func (s *Sessions) DeleteFromRequest(ctx context.Context, r *http.Request) error {
	token := s.TokenFromRequest(r)
	if token == "" {
		return nil
	}
	return s.Delete(ctx, token)
}

// Cookie returns the session cookie holding token. HttpOnly keeps it from
// script, SameSite=Lax keeps it out of cross-site form posts, and Path=/ is
// required by a __Host- name. Max-Age is the TTL, so a browser closed for
// longer does not send a cookie the server would refuse. Callers set it at
// sign-in and again on every request that moves the deadline, so the browser's
// deadline tracks the row's.
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

// ClearedCookie returns a cookie that removes the session cookie from the
// browser. It has the same attributes as Cookie, because a browser only
// replaces a cookie whose name, path and host prefix match.
func (s *Sessions) ClearedCookie() *http.Cookie {
	cookie := s.Cookie("")
	cookie.MaxAge = -1
	return cookie
}

// TokenFromRequest returns the token in the request's session cookie, or the
// empty string when there is none.
func (s *Sessions) TokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(s.cookie.Name)
	if err != nil {
		return ""
	}
	return cookie.Value
}
