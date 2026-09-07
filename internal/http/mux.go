package http

import (
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/build"
	"github.com/ismailshak/sprig/internal/store"
)

// route is one pattern the server handles. An empty capability admits every
// member. New wraps a route with a capability in require, so a handler never
// checks its own.
type route struct {
	pattern    string
	capability auth.Capability
	// withoutGarden marks a route that runs for an account in no garden.
	// Every other protected route renders the "You're in no garden" page
	// instead, so a handler always has a garden to read.
	withoutGarden bool
	// limits are the rate limiters wrapped around this route, outermost first.
	limits  []middleware
	handler http.Handler
}

type middleware func(http.Handler) http.Handler

// routes is every route the server has. New registers from this slice and the
// enforcement test walks it, because http.ServeMux does not list its patterns
// and a route registered directly on the mux would be one the test cannot see.
// devRoutes is what a development build adds, and empty otherwise.
// signupEnabled is SPRIG_SIGNUP_ENABLED, whether Set up your garden is served
// on an install that already has an account. pushKey is the VAPID public key
// the Notifications page gives the browser. It is empty when push is off.
func routes(logger *slog.Logger, sessions *auth.Sessions, passkeys *auth.Passkeys, queries *store.Queries, templates *Templates, assets *Assets, trustedIPHeader string, signupEnabled bool, pushKey string, wake func()) []route {
	todayHandler := &today{logger: logger, queries: queries, templates: templates, now: time.Now}
	plantsHandler := &plants{logger: logger, queries: queries, templates: templates, now: time.Now}
	activityHandler := &activity{logger: logger, queries: queries, templates: templates, now: time.Now}
	// The setup and invite pages use the resolver to tell whether the browser
	// is already signed in.
	resolver := auth.NewResolver(sessions, queries)
	passkeyHandler := &passkeyCeremony{logger: logger, passkeys: passkeys, sessions: sessions, queries: queries, templates: templates, now: time.Now}
	moreHandler := &more{logger: logger, sessions: sessions, queries: queries, templates: templates, build: build.Read(), now: time.Now, pushKey: pushKey, wake: wake}
	setupHandler := &setup{logger: logger, passkeys: passkeys, sessions: sessions, resolver: resolver, queries: queries, templates: templates, now: time.Now, enabled: signupEnabled, wake: wake}
	invitedHandler := &invited{logger: logger, passkeys: passkeys, sessions: sessions, resolver: resolver, queries: queries, templates: templates, now: time.Now, wake: wake}
	base := []route{
		{pattern: "GET /healthz", handler: http.HandlerFunc(handleHealthz)},
		{pattern: assetPattern, handler: assets.handler()},
		{pattern: "GET " + serviceWorkerPath, handler: http.HandlerFunc(newServiceWorker(assets, templates).serve)},
		{pattern: "GET " + offlinePath, handler: offline(templates)},
		{pattern: "GET /{$}", handler: http.HandlerFunc(todayHandler.show)},
		{pattern: "GET /plants", handler: http.HandlerFunc(plantsHandler.show)},
		{pattern: "GET /plants/new", capability: auth.PlantCreate, handler: http.HandlerFunc(plantsHandler.newPlant)},
		{pattern: "POST /plants/new", capability: auth.PlantCreate, handler: http.HandlerFunc(plantsHandler.create)},
		{pattern: "GET /plants/{plant}", handler: http.HandlerFunc(plantsHandler.plant)},
		{pattern: "GET /plants/{plant}/edit", capability: auth.PlantEdit, handler: http.HandlerFunc(plantsHandler.edit)},
		{pattern: "POST /plants/{plant}/edit", capability: auth.PlantEdit, handler: http.HandlerFunc(plantsHandler.update)},
		{pattern: "GET /plants/{plant}/archive", capability: auth.PlantArchive, handler: http.HandlerFunc(plantsHandler.confirmArchive)},
		{pattern: "POST /plants/{plant}/archive", capability: auth.PlantArchive, handler: http.HandlerFunc(plantsHandler.archive)},
		{pattern: "GET /plants/{plant}/schedule/{care}", capability: auth.ScheduleEdit, handler: http.HandlerFunc(plantsHandler.editSchedule)},
		{pattern: "POST /plants/{plant}/schedule/{care}", capability: auth.ScheduleEdit, handler: http.HandlerFunc(plantsHandler.saveSchedule)},
		{pattern: "GET /plants/{plant}/schedule/{care}/remove", capability: auth.ScheduleEdit, handler: http.HandlerFunc(plantsHandler.confirmRemoveSchedule)},
		{pattern: "POST /plants/{plant}/schedule/{care}/remove", capability: auth.ScheduleEdit, handler: http.HandlerFunc(plantsHandler.removeSchedule)},
		{pattern: "GET /activity", handler: http.HandlerFunc(activityHandler.show)},
		{pattern: "GET /plants/{plant}/log", capability: auth.CareLog, handler: http.HandlerFunc(todayHandler.sheet)},
		{pattern: "POST /plants/{plant}/log", capability: auth.CareLog, handler: http.HandlerFunc(todayHandler.log)},
		{pattern: "DELETE /plants/{plant}/log/{event}", capability: auth.CareDeleteOwn, handler: http.HandlerFunc(todayHandler.undo)},
		{pattern: "POST /plants/{plant}/log/{event}/undo", capability: auth.CareDeleteOwn, handler: http.HandlerFunc(todayHandler.undo)},
		{pattern: "GET /plants/{plant}/log/{event}", capability: auth.CareEditOwn, handler: http.HandlerFunc(activityHandler.correct)},
		{pattern: "POST /plants/{plant}/log/{event}", capability: auth.CareEditOwn, handler: http.HandlerFunc(activityHandler.save)},
		{pattern: "POST /plants/{plant}/log/{event}/delete", capability: auth.CareDeleteOwn, handler: http.HandlerFunc(activityHandler.remove)},
		{pattern: "POST /plants/{plant}/log/{event}/restore", capability: auth.CareDeleteOwn, handler: http.HandlerFunc(activityHandler.restore)},
		{pattern: "GET " + morePath, handler: http.HandlerFunc(moreHandler.show)},
		{pattern: "POST " + signOutPath, withoutGarden: true, handler: http.HandlerFunc(moreHandler.signOut)},
		{pattern: "GET " + accountPath, handler: http.HandlerFunc(moreHandler.account)},
		{pattern: "POST " + accountPath, handler: http.HandlerFunc(moreHandler.saveAccount)},
		{pattern: "GET " + recoveryPath, handler: http.HandlerFunc(moreHandler.recovery)},
		{pattern: "GET " + passkeysPath, handler: http.HandlerFunc(moreHandler.passkeys)},
		{pattern: "POST " + passkeysPath + "/{key}/remove", handler: http.HandlerFunc(moreHandler.removePasskey)},
		{pattern: "POST " + registerPath, handler: http.HandlerFunc(passkeyHandler.registerChallenge)},
		{pattern: "POST " + passkeysPath, handler: http.HandlerFunc(passkeyHandler.register)},
		{pattern: "GET " + signInPath, handler: http.HandlerFunc(passkeyHandler.showSignIn)},
		{pattern: "POST " + challengePath, limits: signInLimits(trustedIPHeader, http.HandlerFunc(tooManySignInChallenges)), handler: http.HandlerFunc(passkeyHandler.signInChallenge)},
		{pattern: "POST " + signInPath, limits: signInLimits(trustedIPHeader, http.HandlerFunc(passkeyHandler.tooManySignInAnswers)), handler: http.HandlerFunc(passkeyHandler.signIn)},
		{pattern: "GET " + setupPath, handler: http.HandlerFunc(setupHandler.show)},
		{pattern: "POST " + setupChallengePath, handler: http.HandlerFunc(setupHandler.challenge)},
		{pattern: "POST " + setupPath, handler: http.HandlerFunc(setupHandler.create)},
		{pattern: "GET " + setupSignedInPath, withoutGarden: true, handler: http.HandlerFunc(setupHandler.showSignedIn)},
		{pattern: "POST " + setupSignedInPath, withoutGarden: true, handler: http.HandlerFunc(setupHandler.createSignedIn)},
		{pattern: "GET " + invitedPattern, handler: http.HandlerFunc(invitedHandler.show)},
		{pattern: "POST " + invitedPattern + "/challenge", limits: inviteLimits(trustedIPHeader, http.HandlerFunc(tooManyChallenges)), handler: http.HandlerFunc(invitedHandler.challenge)},
		{pattern: "POST " + invitedPattern, limits: inviteLimits(trustedIPHeader, http.HandlerFunc(invitedHandler.tooManyAnswers)), handler: http.HandlerFunc(invitedHandler.redeem)},
		{pattern: "GET " + acceptPattern, withoutGarden: true, handler: http.HandlerFunc(invitedHandler.showAccept)},
		{pattern: "POST " + acceptPattern, withoutGarden: true, handler: http.HandlerFunc(invitedHandler.accept)},
		{pattern: "GET " + notificationsPath, handler: http.HandlerFunc(moreHandler.notifications)},
		{pattern: "POST " + notificationsPath, handler: http.HandlerFunc(moreHandler.saveNotifications)},
		{pattern: "POST " + subscribePath, handler: http.HandlerFunc(moreHandler.subscribeBrowser)},
		{pattern: "POST " + notificationsPath + "/browsers/{browser}/remove", handler: http.HandlerFunc(moreHandler.removeBrowser)},
		{pattern: "GET " + installPath, handler: http.HandlerFunc(moreHandler.install)},
		{pattern: "GET " + gardensPath, handler: http.HandlerFunc(todayHandler.gardenSheet)},
		{pattern: "POST " + gardensPath, handler: http.HandlerFunc(todayHandler.switchGarden)},
		{pattern: "GET " + gardenPath, capability: auth.GardenEdit, handler: http.HandlerFunc(moreHandler.garden)},
		{pattern: "POST " + gardenPath, capability: auth.GardenEdit, handler: http.HandlerFunc(moreHandler.saveGardenName)},
		{pattern: "GET " + careTypesPath, capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.newCareType)},
		{pattern: "POST " + careTypesPath, capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.createCareType)},
		{pattern: "GET " + careTypesPath + "/{care}", capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.editCareType)},
		{pattern: "POST " + careTypesPath + "/{care}", capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.renameCareType)},
		{pattern: "POST " + careTypesPath + "/{care}/off", capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.turnOffCareType)},
		{pattern: "POST " + careTypesPath + "/{care}/on", capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.turnOnCareType)},
		{pattern: "POST " + careTypesPath + "/{care}/delete", capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.deleteCareType)},
		{pattern: "GET " + peoplePath, capability: auth.MemberManage, handler: http.HandlerFunc(moreHandler.people)},
		{pattern: "POST " + peoplePath, capability: auth.MemberManage, handler: http.HandlerFunc(moreHandler.saveMembers)},
		{pattern: "GET " + invitePath, capability: auth.MemberInvite, handler: http.HandlerFunc(moreHandler.invite)},
		{pattern: "POST " + invitePath, capability: auth.MemberInvite, handler: http.HandlerFunc(moreHandler.createInviteLink)},
		{pattern: "POST " + peoplePath + "/invites/{invite}/revoke", capability: auth.MemberManage, handler: http.HandlerFunc(moreHandler.revokeInvite)},
		{pattern: "GET " + peoplePath + "/{member}/remove", capability: auth.MemberManage, handler: http.HandlerFunc(moreHandler.confirmRemoveMember)},
		{pattern: "POST " + peoplePath + "/{member}/remove", capability: auth.MemberManage, handler: http.HandlerFunc(moreHandler.removeMember)},
		{pattern: "POST " + peoplePath + "/{member}/reenrol", capability: auth.MemberManage, handler: http.HandlerFunc(moreHandler.reenrolMember)},
		{pattern: "GET " + tokensPath, capability: auth.TokenManage, handler: http.HandlerFunc(moreHandler.tokens)},
		{pattern: "POST " + tokensPath, capability: auth.TokenManage, handler: http.HandlerFunc(moreHandler.createToken)},
		{pattern: "POST " + tokensPath + "/{token}/revoke", capability: auth.TokenManage, handler: http.HandlerFunc(moreHandler.revokeToken)},
	}
	return append(base, devRoutes(sessions, queries, templates)...)
}

// signInLimits returns the rate limiters wrapped around one of the two sign-in
// routes. refused is the handler that responds to a request past the budget.
// Both routes are served without a session and a credential id is guessable,
// so they get budgets of their own.
//
// Six a minute from one address is more sign-ins than anybody needs, and that
// limit is the one doing the work. The shared limit of a hundred and twenty a
// minute is only there to stop one stranger filling the ceremony table. It is
// far looser than the ten a minute a recovery code gets, because a tight shared
// limit would let that stranger lock every member out of the app.
func signInLimits(trustedIPHeader string, refused http.Handler) []middleware {
	return []middleware{
		Limit(NewLimiter(rate.Every(time.Minute/6), 6), ClientAddress(trustedIPHeader), refused),
		Limit(NewLimiter(rate.Every(time.Minute/120), 120), AnySource, refused),
	}
}

// inviteLimits returns the rate limiters wrapped around one of the two routes
// that redeem an invite link. refused is the handler that responds to a
// request past the budget. The routes are served without a session, so they
// get budgets of their own.
//
// Six a minute from one address is more attempts than joining takes, and that
// limit is the one doing the work. The shared limit of a hundred and twenty a
// minute only caps the lookups: a token is 256 bits, and a challenge on a link
// that cannot be redeemed writes nothing. A tighter shared limit would stop
// one stranger from stopping everybody else joining.
func inviteLimits(trustedIPHeader string, refused http.Handler) []middleware {
	return []middleware{
		Limit(NewLimiter(rate.Every(time.Minute/6), 6), ClientAddress(trustedIPHeader), refused),
		Limit(NewLimiter(rate.Every(time.Minute/120), 120), AnySource, refused),
	}
}

// publicRoutes is every route served without a session. Authenticate covers
// the rest, so a route in routes is protected until it is listed here.
var publicRoutes = map[string]bool{
	"GET /healthz": true,
	assetPattern:   true,
	// The sign-in page registers the worker too, so it is fetched with no
	// session. The worker caches the offline page as it installs.
	"GET " + serviceWorkerPath: true,
	"GET " + offlinePath:       true,
	// Signing in has to work with no session, so the page, the challenge and
	// the post that signs in are all public.
	"GET " + signInPath:     true,
	"POST " + challengePath: true,
	"POST " + signInPath:    true,
	// The first account is created here, so these cannot require a session.
	// Whether they are served at all is the handler's decision, and a route it
	// closes is a 404.
	"GET " + setupPath:           true,
	"POST " + setupChallengePath: true,
	"POST " + setupPath:          true,
	// An invite link is opened before there is a session, so the page, the
	// challenge and the post that redeems it are all public. The handler
	// returns 404 for a link that cannot be redeemed.
	"GET " + invitedPattern:                 true,
	"POST " + invitedPattern + "/challenge": true,
	"POST " + invitedPattern:                true,
}

// New builds sprig's handler. The middleware order matters. RequestID runs
// outermost so the id is set before anything logs. SecurityHeaders is outside
// Recover so the 500 a panic produces has the security headers too. Logging
// wraps Recover so a recovered panic's 500 still gets a request line. The
// cross-origin check is inside Logging and Recover so a refused request is
// logged like any other. Authentication is inside that so a cross-site post is
// refused before it costs a session lookup. wake is called after a handler
// commits a change to who gets a digest and when. It is nil when push is off.
func New(logger *slog.Logger, sessions *auth.Sessions, passkeys *auth.Passkeys, resolver Resolver, queries *store.Queries, templates *Templates, assets *Assets, trustedIPHeader string, signupEnabled bool, pushKey string, wake func()) http.Handler {
	mux := http.NewServeMux()
	for _, r := range routes(logger, sessions, passkeys, queries, templates, assets, trustedIPHeader, signupEnabled, pushKey, wake) {
		h := r.handler
		if r.capability != "" {
			h = require(r.capability, h)
		}
		// The garden check goes outside the capability check. An account
		// in no garden has no capabilities, so the capability check would
		// return 404 before this page could be rendered.
		if !publicRoutes[r.pattern] && !r.withoutGarden {
			h = requireGarden(templates, signupEnabled, h)
		}
		// Wrapping backwards leaves limits[0] outermost, so a request already
		// refused by the per-address budget spends nothing from the shared one.
		for i := len(r.limits) - 1; i >= 0; i-- {
			h = r.limits[i](h)
		}
		mux.Handle(r.pattern, h)
	}

	isPublic := func(r *http.Request) bool {
		_, pattern := mux.Handler(r)
		return publicRoutes[pattern]
	}

	var handler http.Handler = mux
	handler = Authenticate(logger, sessions, resolver, isPublic)(handler)
	handler = crossOrigin().Handler(handler)
	handler = Recover(logger)(handler)
	handler = Logging(logger, mux)(handler)
	handler = SecurityHeaders(handler)
	handler = RequestID(handler)
	return handler
}

// crossOrigin is the second half of the CSRF defence after SameSite=Lax on the
// session cookie. It refuses an unsafe method whose Sec-Fetch-Site says
// cross-site or whose Origin names a host other than the one the request
// arrived at. The expected origin is the Host the request came in on, so
// nothing is configured and the check holds behind a proxy as well as on a
// laptop.
func crossOrigin() *http.CrossOriginProtection {
	return http.NewCrossOriginProtection()
}
