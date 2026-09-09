package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"golang.org/x/time/rate"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/build"
	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/push"
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
	// bearer marks a route that takes an API token in the Authorization
	// header instead of the session cookie. The token's principal has no
	// capabilities, so such a route names none and uses no mutating method.
	bearer bool
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
// test sends that page's test message to one browser. It is nil when push is
// off.
func routes(logger *slog.Logger, sessions *auth.Sessions, passkeys *auth.Passkeys, queries *store.Queries, photos *photo.Store, templates *Templates, assets *Assets, trustedIPHeader string, signupEnabled bool, pushKey string, wake func(), notify notifyActivity, test sendTest) []route {
	todayHandler := &today{logger: logger, queries: queries, templates: templates, now: time.Now, notify: notify}
	plantsHandler := &plants{logger: logger, queries: queries, photos: photos, templates: templates, now: time.Now}
	activityHandler := &activity{logger: logger, queries: queries, templates: templates, now: time.Now}
	choresHandler := &chores{logger: logger, queries: queries, now: time.Now}
	// The setup and invite pages use the resolver to tell whether the browser
	// is already signed in.
	resolver := auth.NewResolver(sessions, queries)
	passkeyHandler := &passkeyCeremony{logger: logger, passkeys: passkeys, sessions: sessions, queries: queries, templates: templates, now: time.Now}
	moreHandler := &more{logger: logger, sessions: sessions, queries: queries, photos: photos, templates: templates, build: build.Read(), now: time.Now, pushKey: pushKey, wake: wake, test: test}
	setupHandler := &setup{logger: logger, passkeys: passkeys, sessions: sessions, resolver: resolver, queries: queries, templates: templates, now: time.Now, enabled: signupEnabled, wake: wake}
	invitedHandler := &invited{logger: logger, passkeys: passkeys, sessions: sessions, resolver: resolver, queries: queries, templates: templates, now: time.Now, wake: wake}
	recoverHandler := &recoverAccount{logger: logger, passkeys: passkeys, queries: queries, templates: templates, now: time.Now}
	recoverLimit := newRecoverLimits(trustedIPHeader)
	base := []route{
		{pattern: "GET /healthz", handler: http.HandlerFunc(handleHealthz)},
		{pattern: assetPattern, handler: assets.handler()},
		{pattern: "GET " + serviceWorkerPath, handler: http.HandlerFunc(newServiceWorker(assets, templates).serve)},
		{pattern: "GET " + offlinePath, handler: offline(templates)},
		{pattern: "GET /{$}", handler: http.HandlerFunc(todayHandler.show)},
		{pattern: "GET /plants", handler: http.HandlerFunc(plantsHandler.show)},
		{pattern: "GET " + archivedPlantsPath, handler: http.HandlerFunc(plantsHandler.archived)},
		{pattern: "GET /plants/new", capability: auth.PlantCreate, handler: http.HandlerFunc(plantsHandler.newPlant)},
		{pattern: "POST /plants/new", capability: auth.PlantCreate, handler: http.HandlerFunc(plantsHandler.create)},
		{pattern: "GET /plants/{plant}", handler: http.HandlerFunc(plantsHandler.plant)},
		{pattern: "GET /plants/{plant}/edit", capability: auth.PlantEdit, handler: http.HandlerFunc(plantsHandler.edit)},
		{pattern: "POST /plants/{plant}/edit", capability: auth.PlantEdit, handler: http.HandlerFunc(plantsHandler.update)},
		{pattern: "GET /plants/{plant}/archive", capability: auth.PlantArchive, handler: http.HandlerFunc(plantsHandler.confirmArchive)},
		{pattern: "POST /plants/{plant}/archive", capability: auth.PlantArchive, handler: http.HandlerFunc(plantsHandler.archive)},
		{pattern: "POST /plants/{plant}/restore", capability: auth.PlantArchive, handler: http.HandlerFunc(plantsHandler.restore)},
		{pattern: "GET /plants/{plant}/schedule/{care}", capability: auth.ScheduleEdit, handler: http.HandlerFunc(plantsHandler.editSchedule)},
		{pattern: "POST /plants/{plant}/schedule/{care}", capability: auth.ScheduleEdit, handler: http.HandlerFunc(plantsHandler.saveSchedule)},
		{pattern: "GET /plants/{plant}/schedule/{care}/remove", capability: auth.ScheduleEdit, handler: http.HandlerFunc(plantsHandler.confirmRemoveSchedule)},
		{pattern: "POST /plants/{plant}/schedule/{care}/remove", capability: auth.ScheduleEdit, handler: http.HandlerFunc(plantsHandler.removeSchedule)},
		{pattern: "GET /plants/{plant}/photos/{photo}/full", handler: http.HandlerFunc(plantsHandler.photoFull)},
		{pattern: "GET /plants/{plant}/photos/{photo}/square", handler: http.HandlerFunc(plantsHandler.photoSquare)},
		{pattern: "GET /plants/{plant}/photos", handler: http.HandlerFunc(plantsHandler.photoGrid)},
		{pattern: "GET /plants/{plant}/photos/new", capability: auth.PhotoAdd, handler: http.HandlerFunc(plantsHandler.newPhoto)},
		{pattern: "POST /plants/{plant}/photos/new", capability: auth.PhotoAdd, handler: http.HandlerFunc(plantsHandler.addPhoto)},
		{pattern: "GET /plants/{plant}/photos/{photo}", handler: http.HandlerFunc(plantsHandler.photo)},
		{pattern: "GET /plants/{plant}/photos/{photo}/delete", capability: auth.PhotoDeleteOwn, handler: http.HandlerFunc(plantsHandler.confirmDelete)},
		{pattern: "POST /plants/{plant}/photos/{photo}/delete", capability: auth.PhotoDeleteOwn, handler: http.HandlerFunc(plantsHandler.deletePhoto)},
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
		{pattern: "GET " + closeAccountPath, withoutGarden: true, handler: http.HandlerFunc(moreHandler.confirmCloseAccount)},
		{pattern: "POST " + closeAccountPath, withoutGarden: true, handler: http.HandlerFunc(moreHandler.closeAccount)},
		{pattern: "GET " + recoveryPath, handler: http.HandlerFunc(moreHandler.recovery)},
		{pattern: "POST " + recoveryPath, handler: http.HandlerFunc(moreHandler.createCodes)},
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
		{pattern: "GET " + recoverPath, handler: http.HandlerFunc(recoverHandler.show)},
		{pattern: "POST " + recoverPath, limits: recoverLimit.around(http.HandlerFunc(recoverHandler.tooManyCodes)), handler: http.HandlerFunc(recoverHandler.check)},
		{pattern: "POST " + recoverChallengePath, limits: recoverLimit.around(http.HandlerFunc(tooManyRecoveryChallenges)), handler: http.HandlerFunc(recoverHandler.challenge)},
		{pattern: "POST " + recoverPasskeyPath, limits: recoverLimit.around(http.HandlerFunc(recoverHandler.tooManyCodes)), handler: http.HandlerFunc(recoverHandler.register)},
		{pattern: "GET " + acceptPattern, withoutGarden: true, handler: http.HandlerFunc(invitedHandler.showAccept)},
		{pattern: "POST " + acceptPattern, withoutGarden: true, handler: http.HandlerFunc(invitedHandler.accept)},
		{pattern: "GET " + notificationsPath, handler: http.HandlerFunc(moreHandler.notifications)},
		{pattern: "POST " + notificationsPath, handler: http.HandlerFunc(moreHandler.saveNotifications)},
		{pattern: "POST " + subscribePath, handler: http.HandlerFunc(moreHandler.subscribeBrowser)},
		{pattern: "POST " + sendTestPath, handler: http.HandlerFunc(moreHandler.sendTestNotification)},
		{pattern: "POST " + notificationsPath + "/browsers/{browser}/remove", handler: http.HandlerFunc(moreHandler.removeBrowser)},
		{pattern: "GET " + installPath, handler: http.HandlerFunc(moreHandler.install)},
		{pattern: "GET " + gardensPath, handler: http.HandlerFunc(todayHandler.gardenSheet)},
		{pattern: "POST " + gardensPath, handler: http.HandlerFunc(todayHandler.switchGarden)},
		{pattern: "GET " + gardenPath, capability: auth.GardenEdit, handler: http.HandlerFunc(moreHandler.garden)},
		{pattern: "POST " + gardenPath, capability: auth.GardenEdit, handler: http.HandlerFunc(moreHandler.saveGardenName)},
		{pattern: "GET " + deleteGardenPath, capability: auth.GardenDelete, handler: http.HandlerFunc(moreHandler.confirmDeleteGarden)},
		{pattern: "POST " + deleteGardenPath, capability: auth.GardenDelete, handler: http.HandlerFunc(moreHandler.deleteGarden)},
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
		{pattern: "GET " + choresPath, bearer: true, limits: choresLimits(), handler: http.HandlerFunc(choresHandler.show)},
		// Every path no other route matches.
		{pattern: "/", withoutGarden: true, handler: notFoundHandler},
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
	// A recovery code is for somebody with no working passkey, so the page,
	// the post that checks a code, the challenge and the post that saves the
	// passkey are all public. None of them starts a session.
	"GET " + recoverPath:           true,
	"POST " + recoverPath:          true,
	"POST " + recoverChallengePath: true,
	"POST " + recoverPasskeyPath:   true,
}

// New builds sprig's handler. resolver turns a session cookie into a
// principal. tokens turns a bearer token into one. The middleware order
// matters. RequestID runs outermost so the id is set before anything logs.
// SecurityHeaders is outside Recover so the 500 a panic produces has the
// security headers too. Logging wraps Recover so a recovered panic's 500
// still gets a request line. The cross-origin check is inside Logging and
// Recover so a refused request is logged like any other. Authentication is
// inside that so a cross-site post is refused before it costs a session
// lookup. wake is called after a handler commits a change to who gets a
// digest and when. notify is called after a handler records care, to send
// the garden's other members a push notification about it. test sends the
// Notifications page's test message to one browser. All three are nil when
// push is off.
func New(logger *slog.Logger, sessions *auth.Sessions, passkeys *auth.Passkeys, resolver, tokens Resolver, queries *store.Queries, photos *photo.Store, templates *Templates, assets *Assets, trustedIPHeader string, signupEnabled bool, pushKey string, wake func(), notify func(ctx context.Context, gardenID, actorID uuid.UUID, n push.Notification), test func(ctx context.Context, subscription store.PushSubscription, n push.Notification) error) http.Handler {
	mux := http.NewServeMux()
	table := routes(logger, sessions, passkeys, queries, photos, templates, assets, trustedIPHeader, signupEnabled, pushKey, wake, notifyActivity(notify), sendTest(test))
	for _, r := range table {
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
		// The token lookup goes outside the garden check because the token is
		// where the garden comes from. It goes inside the limiters so a request
		// past the budget costs no lookup.
		if r.bearer {
			h = requireToken(logger, tokens, h)
		}
		// Wrapping backwards leaves limits[0] outermost, so a request already
		// refused by the per-address budget spends nothing from the shared one.
		for i := len(r.limits) - 1; i >= 0; i-- {
			h = r.limits[i](h)
		}
		mux.Handle(r.pattern, h)
	}

	credentials := credentialsFor(table)
	credentialFor := func(r *http.Request) credential {
		_, pattern := mux.Handler(r)
		return credentials[pattern]
	}

	var handler http.Handler = mux
	handler = Authenticate(logger, sessions, resolver, credentialFor)(handler)
	handler = crossOrigin().Handler(handler)
	handler = Recover(logger)(handler)
	handler = Logging(logger, mux)(handler)
	handler = SecurityHeaders(handler)
	handler = RequestID(handler)
	return handler
}

// credentialsFor returns what each route's pattern authenticates with. A
// pattern absent from the map, including one no route matches, takes the
// session cookie.
func credentialsFor(table []route) map[string]credential {
	credentials := map[string]credential{}
	for _, r := range table {
		switch {
		case publicRoutes[r.pattern]:
			credentials[r.pattern] = noCredential
		case r.bearer:
			credentials[r.pattern] = apiToken
		}
	}
	return credentials
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
