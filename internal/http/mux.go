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

// Dependencies is what New builds the handler from. Wake, NotifyActivity,
// NotifyUser and SendTest are nil and PushKey is empty when push is off.
type Dependencies struct {
	Logger   *slog.Logger
	Sessions *auth.Sessions
	Passkeys *auth.Passkeys
	// Resolver turns a session cookie into the principal signed in with it.
	// Every route that reads the session cookie uses it, including the Set up
	// your garden page and the page an invite link opens.
	Resolver Resolver
	// Tokens turns an API token in the Authorization header into a principal.
	Tokens    Resolver
	Queries   *store.Queries
	Photos    *photo.Store
	Templates *Templates
	Assets    *Assets
	// TrustedIPHeader is the header a reverse proxy puts the client address
	// in. Empty means RemoteAddr is the client address.
	TrustedIPHeader string
	// SignupEnabled is true when Set up your garden is served on an install
	// that already has an account.
	SignupEnabled bool
	// PushKey is the VAPID public key the Notifications page gives the
	// browser.
	PushKey string
	// Wake has the digest and deadlines jobs work out their next send again.
	Wake func()
	// NotifyActivity sends the garden's other members a push notification
	// about care someone has just logged.
	NotifyActivity func(ctx context.Context, gardenID, actorID uuid.UUID, n push.Notification)
	// NotifyUser sends one person a notification about their access or their
	// garden.
	NotifyUser func(ctx context.Context, user store.AppUser, n push.Notification)
	// SendTest sends the Notifications page's test message to one browser.
	SendTest func(ctx context.Context, subscription store.PushSubscription, n push.Notification) error
}

// routes is every route the server has. New registers from this slice and the
// enforcement test walks it, because http.ServeMux does not list its patterns
// and a route registered directly on the mux would be one the test cannot see.
// devRoutes is what a development build adds, and empty otherwise.
func routes(d Dependencies) []route {
	todayHandler := &today{logger: d.Logger, queries: d.Queries, templates: d.Templates, now: time.Now, notify: d.NotifyActivity, wake: d.Wake, pushKey: d.PushKey}
	plantsHandler := &plants{logger: d.Logger, queries: d.Queries, photos: d.Photos, templates: d.Templates, now: time.Now, notify: d.NotifyUser}
	activityHandler := &activity{logger: d.Logger, queries: d.Queries, templates: d.Templates, now: time.Now}
	choresHandler := &chores{logger: d.Logger, queries: d.Queries, templates: d.Templates, now: time.Now}
	passkeyHandler := &passkeyCeremony{logger: d.Logger, passkeys: d.Passkeys, sessions: d.Sessions, queries: d.Queries, templates: d.Templates, now: time.Now}
	moreHandler := &more{logger: d.Logger, sessions: d.Sessions, queries: d.Queries, templates: d.Templates, build: build.Read(), now: time.Now}
	accountHandler := &account{logger: d.Logger, queries: d.Queries, templates: d.Templates, wake: d.Wake}
	closeAccountHandler := &closeAccount{logger: d.Logger, sessions: d.Sessions, queries: d.Queries, templates: d.Templates, now: time.Now, wake: d.Wake}
	recoveryCodesHandler := &recoveryCodes{logger: d.Logger, queries: d.Queries, templates: d.Templates, now: time.Now}
	passkeysHandler := &passkeys{logger: d.Logger, queries: d.Queries, templates: d.Templates, now: time.Now}
	notificationsHandler := &notifications{logger: d.Logger, queries: d.Queries, templates: d.Templates, now: time.Now, pushKey: d.PushKey, wake: d.Wake, test: d.SendTest}
	gardenHandler := &garden{logger: d.Logger, queries: d.Queries, photos: d.Photos, templates: d.Templates}
	deleteGardenHandler := &deleteGarden{logger: d.Logger, queries: d.Queries, photos: d.Photos, templates: d.Templates, wake: d.Wake}
	peopleHandler := &people{logger: d.Logger, queries: d.Queries, templates: d.Templates, now: time.Now, wake: d.Wake, notify: d.NotifyUser}
	inviteHandler := &invite{logger: d.Logger, queries: d.Queries, templates: d.Templates, now: time.Now}
	tokensHandler := &tokens{logger: d.Logger, queries: d.Queries, templates: d.Templates, now: time.Now, wake: d.Wake}
	setupHandler := &setup{logger: d.Logger, passkeys: d.Passkeys, sessions: d.Sessions, resolver: d.Resolver, queries: d.Queries, templates: d.Templates, now: time.Now, enabled: d.SignupEnabled, wake: d.Wake, pushKey: d.PushKey}
	invitedHandler := &invited{logger: d.Logger, passkeys: d.Passkeys, sessions: d.Sessions, resolver: d.Resolver, queries: d.Queries, templates: d.Templates, now: time.Now, wake: d.Wake, notify: d.NotifyUser}
	recoverHandler := &recoverAccount{logger: d.Logger, passkeys: d.Passkeys, queries: d.Queries, templates: d.Templates, now: time.Now}
	handleHandler := &handleSuggestions{queries: d.Queries, templates: d.Templates, logger: d.Logger}
	recoverLimit := newRecoverLimits(d.TrustedIPHeader)
	base := []route{
		{pattern: healthzPattern, handler: http.HandlerFunc(handleHealthz)},
		{pattern: assetPattern, handler: d.Assets.handler()},
		{pattern: "GET " + serviceWorkerPath, handler: http.HandlerFunc(newServiceWorker(d.Assets, d.Templates).serve)},
		{pattern: "GET " + offlinePath, handler: offline(d.Templates)},
		{pattern: "GET /{$}", handler: http.HandlerFunc(todayHandler.show)},
		{pattern: "POST " + RemindAgainPath, handler: http.HandlerFunc(todayHandler.remindAgain)},
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
		{pattern: "GET " + calendarPath, handler: http.HandlerFunc(activityHandler.calendar)},
		{pattern: "GET " + notesPath + "/new", capability: auth.CalendarNoteManage, handler: http.HandlerFunc(activityHandler.newNote)},
		{pattern: "POST " + notesPath, capability: auth.CalendarNoteManage, handler: http.HandlerFunc(activityHandler.createNote)},
		{pattern: "GET " + notesPath + "/{note}", capability: auth.CalendarNoteManage, handler: http.HandlerFunc(activityHandler.editNote)},
		{pattern: "POST " + notesPath + "/{note}", capability: auth.CalendarNoteManage, handler: http.HandlerFunc(activityHandler.saveNote)},
		{pattern: "POST " + notesPath + "/{note}/delete", capability: auth.CalendarNoteManage, handler: http.HandlerFunc(activityHandler.deleteNote)},
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
		{pattern: "GET " + accountPath, handler: http.HandlerFunc(accountHandler.show)},
		{pattern: "POST " + accountPath, handler: http.HandlerFunc(accountHandler.saveAccount)},
		{pattern: "GET " + closeAccountPath, withoutGarden: true, handler: http.HandlerFunc(closeAccountHandler.confirm)},
		{pattern: "POST " + closeAccountPath, withoutGarden: true, handler: http.HandlerFunc(closeAccountHandler.close)},
		{pattern: "GET " + recoveryPath, handler: http.HandlerFunc(recoveryCodesHandler.show)},
		{pattern: "POST " + recoveryPath, handler: http.HandlerFunc(recoveryCodesHandler.createCodes)},
		{pattern: "GET " + passkeysPath, handler: http.HandlerFunc(passkeysHandler.show)},
		{pattern: "POST " + passkeysPath + "/{key}/remove", handler: http.HandlerFunc(passkeysHandler.removePasskey)},
		{pattern: "POST " + registerPath, handler: http.HandlerFunc(passkeyHandler.registerChallenge)},
		{pattern: "POST " + passkeysPath, handler: http.HandlerFunc(passkeyHandler.register)},
		{pattern: "GET " + signInPath, handler: http.HandlerFunc(passkeyHandler.showSignIn)},
		{pattern: "POST " + challengePath, limits: signInLimits(d.TrustedIPHeader, http.HandlerFunc(tooManySignInChallenges)), handler: http.HandlerFunc(passkeyHandler.signInChallenge)},
		{pattern: "POST " + signInPath, limits: signInLimits(d.TrustedIPHeader, http.HandlerFunc(passkeyHandler.tooManySignInAnswers)), handler: http.HandlerFunc(passkeyHandler.signIn)},
		{pattern: "GET " + setupPath, handler: http.HandlerFunc(setupHandler.show)},
		{pattern: "POST " + setupChallengePath, handler: http.HandlerFunc(setupHandler.challenge)},
		{pattern: "POST " + setupPath, handler: http.HandlerFunc(setupHandler.create)},
		{pattern: "GET " + handlePath, limits: handleLimits(d.TrustedIPHeader), handler: http.HandlerFunc(handleHandler.suggest)},
		{pattern: "GET " + setupSignedInPath, withoutGarden: true, handler: http.HandlerFunc(setupHandler.showSignedIn)},
		{pattern: "POST " + setupSignedInPath, withoutGarden: true, handler: http.HandlerFunc(setupHandler.createSignedIn)},
		{pattern: "GET " + remindersPath, handler: http.HandlerFunc(setupHandler.reminders)},
		{pattern: "GET " + invitedPattern, handler: http.HandlerFunc(invitedHandler.show)},
		{pattern: "POST " + invitedPattern + "/challenge", limits: inviteLimits(d.TrustedIPHeader, http.HandlerFunc(tooManyChallenges)), handler: http.HandlerFunc(invitedHandler.challenge)},
		{pattern: "POST " + invitedPattern, limits: inviteLimits(d.TrustedIPHeader, http.HandlerFunc(invitedHandler.tooManyAnswers)), handler: http.HandlerFunc(invitedHandler.redeem)},
		{pattern: "GET " + recoverPath, handler: http.HandlerFunc(recoverHandler.show)},
		{pattern: "POST " + recoverPath, limits: recoverLimit.around(http.HandlerFunc(recoverHandler.tooManyCodes)), handler: http.HandlerFunc(recoverHandler.check)},
		{pattern: "POST " + recoverChallengePath, limits: recoverLimit.around(http.HandlerFunc(tooManyRecoveryChallenges)), handler: http.HandlerFunc(recoverHandler.challenge)},
		{pattern: "POST " + recoverPasskeyPath, limits: recoverLimit.around(http.HandlerFunc(recoverHandler.tooManyCodes)), handler: http.HandlerFunc(recoverHandler.register)},
		{pattern: "GET " + acceptPattern, withoutGarden: true, handler: http.HandlerFunc(invitedHandler.showAccept)},
		{pattern: "POST " + acceptPattern, withoutGarden: true, handler: http.HandlerFunc(invitedHandler.accept)},
		{pattern: "GET " + notificationsPath, handler: http.HandlerFunc(notificationsHandler.show)},
		{pattern: "POST " + notificationsPath, handler: http.HandlerFunc(notificationsHandler.saveNotifications)},
		{pattern: "POST " + subscribePath, handler: http.HandlerFunc(notificationsHandler.subscribeBrowser)},
		{pattern: "POST " + sendTestPath, handler: http.HandlerFunc(notificationsHandler.sendTestNotification)},
		{pattern: "POST " + notificationsPath + "/browsers/{browser}/remove", handler: http.HandlerFunc(notificationsHandler.removeBrowser)},
		{pattern: "GET " + installPath, handler: install(d.Templates)},
		{pattern: "GET " + appearancePath, handler: appearance(d.Templates)},
		{pattern: "GET " + gardensPath, handler: http.HandlerFunc(todayHandler.gardenSheet)},
		{pattern: "POST " + gardensPath, handler: http.HandlerFunc(todayHandler.switchGarden)},
		{pattern: "GET " + gardenPath, capability: auth.GardenEdit, handler: http.HandlerFunc(gardenHandler.show)},
		{pattern: "POST " + gardenPath, capability: auth.GardenEdit, handler: http.HandlerFunc(gardenHandler.saveGardenName)},
		{pattern: "GET " + deleteGardenPath, capability: auth.GardenDelete, handler: http.HandlerFunc(deleteGardenHandler.confirm)},
		{pattern: "POST " + deleteGardenPath, capability: auth.GardenDelete, handler: http.HandlerFunc(deleteGardenHandler.delete)},
		{pattern: "GET " + careTypesPath, capability: auth.CareTypeManage, handler: http.HandlerFunc(gardenHandler.newCareType)},
		{pattern: "POST " + careTypesPath, capability: auth.CareTypeManage, handler: http.HandlerFunc(gardenHandler.createCareType)},
		{pattern: "GET " + careTypesPath + "/{care}", capability: auth.CareTypeManage, handler: http.HandlerFunc(gardenHandler.editCareType)},
		{pattern: "POST " + careTypesPath + "/{care}", capability: auth.CareTypeManage, handler: http.HandlerFunc(gardenHandler.renameCareType)},
		{pattern: "POST " + careTypesPath + "/{care}/off", capability: auth.CareTypeManage, handler: http.HandlerFunc(gardenHandler.turnOffCareType)},
		{pattern: "POST " + careTypesPath + "/{care}/on", capability: auth.CareTypeManage, handler: http.HandlerFunc(gardenHandler.turnOnCareType)},
		{pattern: "POST " + careTypesPath + "/{care}/delete", capability: auth.CareTypeManage, handler: http.HandlerFunc(gardenHandler.deleteCareType)},
		{pattern: "GET " + PeoplePath, capability: auth.MemberManage, handler: http.HandlerFunc(peopleHandler.show)},
		{pattern: "POST " + PeoplePath, capability: auth.MemberManage, handler: http.HandlerFunc(peopleHandler.saveMembers)},
		{pattern: "GET " + invitePath, capability: auth.MemberInvite, handler: http.HandlerFunc(inviteHandler.show)},
		{pattern: "POST " + invitePath, capability: auth.MemberInvite, handler: http.HandlerFunc(inviteHandler.createInviteLink)},
		{pattern: "POST " + PeoplePath + "/invites/{invite}/revoke", capability: auth.MemberManage, handler: http.HandlerFunc(peopleHandler.revokeInvite)},
		{pattern: "GET " + PeoplePath + "/{member}/remove", capability: auth.MemberManage, handler: http.HandlerFunc(peopleHandler.confirmRemoveMember)},
		{pattern: "POST " + PeoplePath + "/{member}/remove", capability: auth.MemberManage, handler: http.HandlerFunc(peopleHandler.removeMember)},
		{pattern: "POST " + PeoplePath + "/{member}/reenrol", capability: auth.MemberManage, handler: http.HandlerFunc(peopleHandler.reenrolMember)},
		{pattern: "GET " + TokensPath, capability: auth.TokenManage, handler: http.HandlerFunc(tokensHandler.show)},
		{pattern: "POST " + TokensPath, capability: auth.TokenManage, handler: http.HandlerFunc(tokensHandler.createToken)},
		{pattern: "POST " + TokensPath + "/{token}/revoke", capability: auth.TokenManage, handler: http.HandlerFunc(tokensHandler.revokeToken)},
		{pattern: "GET " + choresPath, bearer: true, limits: choresLimits(), handler: http.HandlerFunc(choresHandler.show)},
		// Every path no other route matches.
		{pattern: "/", withoutGarden: true, handler: http.HandlerFunc(d.Templates.notFound)},
	}
	return append(base, devRoutes(d.Sessions, d.Queries, d.Templates)...)
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

// handleLimits returns the rate limiters wrapped around the handle suggestion.
// The route is served without a session and its response says whether a handle
// is held, so it gets budgets of its own. A form asks once each time the
// display name is left, so 30 a minute from one address is more than a person
// editing their name produces. The shared 300 a minute caps how fast a
// stranger can list handles.
func handleLimits(trustedIPHeader string) []middleware {
	refused := http.HandlerFunc(tooManyHandleSuggestions)
	return []middleware{
		Limit(NewLimiter(rate.Every(time.Minute/30), 30), ClientAddress(trustedIPHeader), refused),
		Limit(NewLimiter(rate.Every(time.Minute/300), 300), AnySource, refused),
	}
}

// publicRoutes is every route served without a session. Authenticate covers
// the rest, so a route in routes is protected until it is listed here.
var publicRoutes = map[string]bool{
	healthzPattern: true,
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
	// The handle suggestion fills a field on the two setup forms above and on
	// an invite's join form. All three come before there is a session.
	"GET " + handlePath: true,
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

// New builds sprig's handler. The middleware order matters. RequestID runs
// outermost so the id is set before anything logs. MatchPattern is next,
// because Logging and Authenticate read the route pattern it puts on the
// context. SecurityHeaders is outside Recover so the 500 a panic produces has
// the security headers too. Logging wraps Recover so a recovered panic's 500
// still gets a request line. The cross-origin check is inside Logging and
// Recover so a refused request is logged like any other. Authentication is
// inside that so a cross-site post is refused before it costs a session
// lookup.
func New(d Dependencies) http.Handler {
	mux := http.NewServeMux()
	table := routes(d)
	for _, r := range table {
		h := r.handler
		if r.capability != "" {
			h = require(r.capability, d.Templates, h)
		}
		// The garden check goes outside the capability check. An account
		// in no garden has no capabilities, so the capability check would
		// return 404 before this page could be rendered.
		if !publicRoutes[r.pattern] && !r.withoutGarden {
			h = requireGarden(d.Templates, d.SignupEnabled, h)
		}
		// The token lookup goes outside the garden check because the token is
		// where the garden comes from. It goes inside the limiters so a request
		// past the budget costs no lookup.
		if r.bearer {
			h = requireToken(d.Logger, d.Templates, d.Tokens, h)
		}
		// Wrapping backwards leaves limits[0] outermost, so a request already
		// refused by the per-address budget spends nothing from the shared one.
		for i := len(r.limits) - 1; i >= 0; i-- {
			h = r.limits[i](h)
		}
		mux.Handle(r.pattern, h)
	}

	var handler http.Handler = mux
	handler = Authenticate(d.Logger, d.Templates, d.Sessions, d.Resolver, credentialsFor(table))(handler)
	handler = crossOrigin().Handler(handler)
	handler = Recover(d.Logger, d.Templates)(handler)
	handler = Logging(d.Logger)(handler)
	handler = SecurityHeaders(handler)
	handler = MatchPattern(mux)(handler)
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
