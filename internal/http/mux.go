package http

import (
	"log/slog"
	"net/http"
	"time"

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
	handler    http.Handler
}

// routes is every route the server has. New registers from this slice and the
// enforcement test walks it, because http.ServeMux does not list its patterns
// and a route registered directly on the mux would be one the test cannot see.
// devRoutes is what a development build adds, and empty otherwise.
func routes(logger *slog.Logger, sessions *auth.Sessions, queries *store.Queries, templates *Templates, assets *Assets) []route {
	todayHandler := &today{logger: logger, queries: queries, templates: templates, now: time.Now}
	plantsHandler := &plants{logger: logger, queries: queries, templates: templates, now: time.Now}
	activityHandler := &activity{logger: logger, queries: queries, templates: templates, now: time.Now}
	moreHandler := &more{logger: logger, sessions: sessions, queries: queries, templates: templates, build: build.Read(), now: time.Now}
	base := []route{
		{pattern: "GET /healthz", handler: http.HandlerFunc(handleHealthz)},
		{pattern: assetPattern, handler: assets.handler()},
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
		{pattern: "POST " + signOutPath, handler: http.HandlerFunc(moreHandler.signOut)},
		{pattern: "GET " + accountPath, handler: http.HandlerFunc(moreHandler.account)},
		{pattern: "POST " + accountPath, handler: http.HandlerFunc(moreHandler.saveAccount)},
		{pattern: "GET " + recoveryPath, handler: http.HandlerFunc(moreHandler.recovery)},
		{pattern: "GET " + passkeysPath, handler: http.HandlerFunc(moreHandler.passkeys)},
		{pattern: "POST " + passkeysPath + "/{key}/remove", handler: http.HandlerFunc(moreHandler.removePasskey)},
		{pattern: "GET " + notificationsPath, handler: http.HandlerFunc(moreHandler.notifications)},
		{pattern: "POST " + notificationsPath, handler: http.HandlerFunc(moreHandler.saveNotifications)},
		{pattern: "POST " + notificationsPath + "/browsers/{browser}/remove", handler: http.HandlerFunc(moreHandler.removeBrowser)},
		{pattern: "GET " + installPath, handler: http.HandlerFunc(moreHandler.install)},
		{pattern: "GET " + gardenPath, capability: auth.GardenEdit, handler: http.HandlerFunc(moreHandler.garden)},
		{pattern: "POST " + gardenPath, capability: auth.GardenEdit, handler: http.HandlerFunc(moreHandler.saveGardenName)},
		{pattern: "GET " + careTypesPath, capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.newCareType)},
		{pattern: "POST " + careTypesPath, capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.createCareType)},
		{pattern: "GET " + careTypesPath + "/{care}", capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.editCareType)},
		{pattern: "POST " + careTypesPath + "/{care}", capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.renameCareType)},
		{pattern: "POST " + careTypesPath + "/{care}/off", capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.turnOffCareType)},
		{pattern: "POST " + careTypesPath + "/{care}/on", capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.turnOnCareType)},
		{pattern: "POST " + careTypesPath + "/{care}/delete", capability: auth.CareTypeManage, handler: http.HandlerFunc(moreHandler.deleteCareType)},
	}
	return append(base, devRoutes(sessions, queries, templates)...)
}

// publicRoutes is every route served without a session. Authenticate covers
// the rest, so a route in routes is protected until it is listed here.
var publicRoutes = map[string]bool{
	"GET /healthz": true,
	assetPattern:   true,
}

// New builds sprig's handler. The middleware order matters. RequestID runs
// outermost so the id is set before anything logs. Logging wraps Recover so a
// recovered panic's 500 still gets a request line. The cross-origin check is
// inside both so a refused request is logged like any other. Authentication is
// inside that so a cross-site post is refused before it costs a session lookup.
func New(logger *slog.Logger, sessions *auth.Sessions, resolver Resolver, queries *store.Queries, templates *Templates, assets *Assets) http.Handler {
	mux := http.NewServeMux()
	for _, r := range routes(logger, sessions, queries, templates, assets) {
		h := r.handler
		if r.capability != "" {
			h = require(r.capability, h)
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
