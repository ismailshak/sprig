package http

import (
	"maps"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
)

// setupLinkOn returns the setup fixture with sign-up off after Robin has set
// up Greenhouse, so /setup is a 404, and one setup link issued at thursday. It
// also returns Robin's session token and the link's token.
func setupLinkOn(t *testing.T) (f *setupFixture, session, link string) {
	t.Helper()

	f = setupOn(t, false)
	session = f.mustCreate(t, aGardenForm(), aDevice())
	made, err := auth.IssueSetupLink(t.Context(), f.queries, thursday)
	if err != nil {
		t.Fatalf("issuing the setup link: %v", err)
	}
	return f, session, made.Token
}

func samsGardenForm() url.Values {
	return url.Values{"garden": {"Allotment"}, "name": {"Sam"}, "timezone": {"Europe/London"}}
}

// linkOpen reports whether the setup link with token can still be used at
// thursday.
func (f *setupFixture) linkOpen(t *testing.T, token string) bool {
	t.Helper()

	open, err := f.queries.SetupLinkOpen(t.Context(), auth.HashToken(token), thursday)
	if err != nil {
		t.Fatalf("looking up the setup link: %v", err)
	}
	return open
}

func TestSetupLink_WithSignUpOffTheLinkShowsAFormPostingToItAndASignInLink(t *testing.T) {
	f, _, link := setupLinkOn(t)

	rec := f.requestOnLink(t, f.handler.show, link, SetupLinkPath(link), nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	form := passkeyForm(t, page, SetupLinkPath(link), setupLinkChallengePath(link))
	for _, field := range []string{"garden", "name", "timezone"} {
		if got := form.byID(field).attr("name"); got != field {
			t.Errorf("the %s field posts as %q, want %s:\n%s", field, got, field, form)
		}
	}
	if !linkTo(page, signInToSetUpPath(link), "Sign in") {
		t.Errorf("the page has no link to sign in with the next path set to %s:\n%s", setupSignedInPathFor(link), page)
	}
}

func TestSetupLink_WithSignUpOffASignedOutVisitorGetsAnAccountAndAGardenAndTheLinkIsUsed(t *testing.T) {
	f, _, link := setupLinkOn(t)
	before := f.counts(t)

	rec := f.createOnLink(t, link, samsGardenForm(), aDevice())

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != remindersPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, remindersPath, text(rec.Body.String()))
	}
	session := cookieNamed(t, rec, "__Host-sprig_session")
	if session == nil {
		t.Fatal("the post set no session cookie")
	}
	sam := f.principalOf(t, session.Value)
	if sam.User.DisplayName != "Sam" || sam.Garden.Name != "Allotment" || sam.Membership.Role != "owner" {
		t.Errorf("the session is %s's on %q as %s, want Sam's on Allotment as owner", sam.User.DisplayName, sam.Garden.Name, sam.Membership.Role)
	}
	after := f.counts(t)
	for table, more := range map[string]int{"app_user": 1, "garden": 1, "membership": 1, "care_type": 3, "passkey_credential": 1} {
		if after[table] != before[table]+more {
			t.Errorf("%s has %d rows, want %d", table, after[table], before[table]+more)
		}
	}
	if f.linkOpen(t, link) {
		t.Error("the setup link can still be used")
	}
	if rec := f.request(t, f.handler.show, setupPath, nil); rec.Code != http.StatusNotFound {
		t.Errorf("/setup after the link was used: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestSetupLink_AUsedExpiredOrUnknownLinkIs404OnEveryRouteAndWritesNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		// invalidate makes the fixture's link unusable and returns the token
		// to request with.
		invalidate func(t *testing.T, f *setupFixture, link string) string
	}{
		{"used", func(t *testing.T, f *setupFixture, link string) string {
			if _, err := f.queries.UseSetupLink(t.Context(), thursday, auth.HashToken(link)); err != nil {
				t.Fatalf("using the link: %v", err)
			}
			return link
		}},
		{"expired", func(_ *testing.T, f *setupFixture, link string) string {
			f.handler.now = func() time.Time { return thursday.Add(auth.InviteLifetime) }
			return link
		}},
		{"unknown", func(_ *testing.T, _ *setupFixture, _ string) string {
			return auth.NewInviteToken()
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, session, link := setupLinkOn(t)
			robin := f.principalOf(t, session)
			token := c.invalidate(t, f, link)
			before := f.counts(t)

			for _, route := range []struct {
				name   string
				status func() int
			}{
				{"the page", func() int {
					return f.requestOnLink(t, f.handler.show, token, SetupLinkPath(token), nil).Code
				}},
				{"the challenge", func() int {
					return f.requestOnLink(t, f.handler.challenge, token, setupLinkChallengePath(token), samsGardenForm()).Code
				}},
				{"the post", func() int {
					return f.requestOnLink(t, f.handler.create, token, SetupLinkPath(token), samsGardenForm()).Code
				}},
				{"the signed-in page", func() int {
					return f.asAccountOnLink(t, f.handler.showSignedIn, token, robin, nil).Code
				}},
				{"the signed-in post", func() int {
					return f.asAccountOnLink(t, f.handler.createSignedIn, token, robin, url.Values{"garden": {"Allotment"}}).Code
				}},
			} {
				if code := route.status(); code != http.StatusNotFound {
					t.Errorf("%s: status = %d, want %d", route.name, code, http.StatusNotFound)
				}
			}
			if after := f.counts(t); !maps.Equal(before, after) {
				t.Errorf("the routes wrote rows: before %v, after %v", before, after)
			}
		})
	}
}

func TestSetupLink_TwoPostsWithOneLinkCreateOneGarden(t *testing.T) {
	f, _, link := setupLinkOn(t)
	// Both challenges are issued while the link is unused, as two browsers
	// on the page at once would get them.
	sam := samsGardenForm()
	samCreation, samCookie := f.challengeOnLink(t, link, sam)
	kim := url.Values{"garden": {"Balcony"}, "name": {"Kim"}, "timezone": {"Europe/London"}}
	kimCreation, kimCookie := f.challengeOnLink(t, link, kim)
	gardens := f.count(t, "garden")

	sam.Set(credentialField, aDevice().Register(samCreation))
	if rec := f.requestOnLink(t, f.handler.create, link, SetupLinkPath(link), sam, samCookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("the first post: status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, text(rec.Body.String()))
	}
	kim.Set(credentialField, aDevice().Register(kimCreation))
	rec := f.requestOnLink(t, f.handler.create, link, SetupLinkPath(link), kim, kimCookie)

	if rec.Code != http.StatusNotFound {
		t.Errorf("the second post: status = %d, want %d:\n%s", rec.Code, http.StatusNotFound, text(rec.Body.String()))
	}
	if n := f.count(t, "garden"); n != gardens+1 {
		t.Errorf("%d gardens, want %d", n, gardens+1)
	}
}

func TestSetupLink_ALinkThatExpiresBetweenTheCheckAndTheWriteIs404AndWritesNoGarden(t *testing.T) {
	f, _, link := setupLinkOn(t)
	expires := thursday.Add(auth.InviteLifetime)
	// The challenge reads the clock in open and BeginSetup, and the post in open
	// and TakeSetup. Those four reads get the link's last second, so the link
	// and the ceremony are both open. The fifth read is claim's, inside the
	// transaction, and gets the instant the link expires.
	reads := 0
	f.handler.now = func() time.Time {
		reads++
		if reads <= 4 {
			return expires.Add(-time.Second)
		}
		return expires
	}
	gardens := f.count(t, "garden")

	rec := f.createOnLink(t, link, samsGardenForm(), aDevice())

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d:\n%s", rec.Code, http.StatusNotFound, text(rec.Body.String()))
	}
	if n := f.count(t, "garden"); n != gardens {
		t.Errorf("%d gardens, want %d", n, gardens)
	}
	// The post deletes the ceremony row after open passes, so an empty table
	// shows the 404 came from the transaction and not from open.
	if n := f.count(t, "webauthn_ceremony"); n != 0 {
		t.Errorf("%d ceremonies left, want 0: the post was refused before it reached the transaction", n)
	}
}

func TestSetupLink_ASignedInBrowserIsSentToTheLinksSignedInPage(t *testing.T) {
	f, session, link := setupLinkOn(t)

	rec := f.requestOnLink(t, f.handler.show, link, SetupLinkPath(link), nil, &http.Cookie{Name: "__Host-sprig_session", Value: session})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != setupSignedInPathFor(link) {
		t.Errorf("status = %d, Location = %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, setupSignedInPathFor(link))
	}
}

func TestSetupLink_WithSignUpOffASignedInAccountGetsAGardenItOwnsAndLandsOnToday(t *testing.T) {
	f, session, link := setupLinkOn(t)
	robin := f.principalOf(t, session)

	page := f.asAccountOnLink(t, f.handler.showSignedIn, link, robin, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the page: status = %d, want %d:\n%s", page.Code, http.StatusOK, page.Body.String())
	}
	if form := formTo(page.Body.String(), setupSignedInPathFor(link)); form.attr("method") != "post" {
		t.Errorf("the page has no form posting to %s:\n%s", setupSignedInPathFor(link), page.Body.String())
	}
	before := f.counts(t)

	rec := f.asAccountOnLink(t, f.handler.createSignedIn, link, robin, url.Values{"garden": {"Allotment"}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath, text(rec.Body.String()))
	}
	now := f.principalOf(t, session)
	if now.User.ID != robin.User.ID || now.Garden.Name != "Allotment" || now.Membership.Role != "owner" {
		t.Errorf("the session is %s's on %q as %s, want Robin's on Allotment as owner", now.User.DisplayName, now.Garden.Name, now.Membership.Role)
	}
	after := f.counts(t)
	for table, more := range map[string]int{"app_user": 0, "passkey_credential": 0, "garden": 1, "membership": 1} {
		if after[table] != before[table]+more {
			t.Errorf("%s has %d rows, want %d", table, after[table], before[table]+more)
		}
	}
	if f.linkOpen(t, link) {
		t.Error("the setup link can still be used")
	}
}
