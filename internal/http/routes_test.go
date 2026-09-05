package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

// access is the second statement of what a route requires, kept apart from
// routes in mux.go so that adding a route means deciding its access twice and
// the test compares the two.
type access struct {
	public     bool
	capability auth.Capability
	// anyMember marks a mutating route that carries no capability on purpose.
	anyMember bool
	// path is a request path the pattern matches, carrying Rosewood's ids.
	path string
	// foreign is path with one of Fairview's ids in place of Rosewood's. The
	// route answers 404 to it, because the store has no read that finds another
	// garden's row from a session on Rosewood.
	foreign string
}

var routeAccess = map[string]access{
	"GET /healthz": {public: true},
	// The hashed URL is known only at startup, so this path is a plain one the
	// pattern matches.
	assetPattern:               {public: true, path: assetPrefix + "app.css"},
	"GET /{$}":                 {},
	"GET /plants":              {},
	"GET /plants/{plant}/log":  {capability: auth.CareLog, path: logPath(rosewoodPlantID), foreign: logPath(fairviewPlantID)},
	"POST /plants/{plant}/log": {capability: auth.CareLog, path: logPath(rosewoodPlantID), foreign: logPath(fairviewPlantID)},
	"DELETE /plants/{plant}/log/{event}": {
		capability: auth.CareDeleteOwn,
		path:       undoPath(rosewoodPlantID, rosewoodEventID, "water"),
		foreign:    undoPath(fairviewPlantID, fairviewEventID, "water"),
	},
	"POST /plants/{plant}/log/{event}/undo": {
		capability: auth.CareDeleteOwn,
		path:       undoFormPath(rosewoodPlantID, rosewoodUndoEventID),
		foreign:    undoFormPath(fairviewPlantID, fairviewUndoEventID),
	},
}

var (
	fairviewID      = uuid.MustParse("00000000-0000-7000-8000-000000000201")
	rosewoodPlantID = uuid.MustParse("00000000-0000-7000-8000-000000000211")
	fairviewPlantID = uuid.MustParse("00000000-0000-7000-8000-000000000212")
	rosewoodEventID = uuid.MustParse("00000000-0000-7000-8000-000000000221")
	fairviewEventID = uuid.MustParse("00000000-0000-7000-8000-000000000222")
	// The two routes that delete an event take one each because the first to
	// run would leave the second a 404.
	rosewoodUndoEventID = uuid.MustParse("00000000-0000-7000-8000-000000000223")
	fairviewUndoEventID = uuid.MustParse("00000000-0000-7000-8000-000000000224")
)

// routeQueries seeds two gardens, each with a scheduled plant and two care
// events, because a route naming an object needs one in sitterPrincipal's
// garden and one outside it. The events are the principal's own because the
// route that deletes one answers a caller who may delete only their own.
func routeQueries(t *testing.T) *store.Queries {
	t.Helper()

	ctx := t.Context()
	tx := pgtest.Tx(t, migrateSchema)
	rosewoodID := sitterPrincipal().Garden.ID
	seed := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO garden (id, name) VALUES ($1, 'Rosewood'), ($2, 'Fairview')", []any{rosewoodID, fairviewID}},
		{"INSERT INTO care_type (garden_id, name, slug) VALUES ($1, 'Water', 'water'), ($2, 'Water', 'water')", []any{rosewoodID, fairviewID}},
		{"INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Big Fella'), ($3, $4, 'Gerald')", []any{rosewoodPlantID, rosewoodID, fairviewPlantID, fairviewID}},
		{`INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit)
			SELECT garden_id, $1, id, 7, 'day' FROM care_type WHERE garden_id = $2`, []any{rosewoodPlantID, rosewoodID}},
		{`INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit)
			SELECT garden_id, $1, id, 7, 'day' FROM care_type WHERE garden_id = $2`, []any{fairviewPlantID, fairviewID}},
		{"INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Ellie', 'ellie', 'Europe/London')", []any{sitterPrincipal().User.ID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4`, []any{rosewoodEventID, rosewoodPlantID, sitterPrincipal().User.ID, rosewoodID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4`, []any{fairviewEventID, fairviewPlantID, sitterPrincipal().User.ID, fairviewID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4`, []any{rosewoodUndoEventID, rosewoodPlantID, sitterPrincipal().User.ID, rosewoodID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4`, []any{fairviewUndoEventID, fairviewPlantID, sitterPrincipal().User.ID, fairviewID}},
	}
	for _, row := range seed {
		if _, err := tx.Exec(ctx, row.sql, row.args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, row.sql)
		}
	}
	return store.New(tx)
}

func TestRoutes_EveryRouteHasOneEntryAndTheTwoAgree(t *testing.T) {
	table := routes(testLogger, testSessions(), nil, testTemplates(), testAssets())
	patterns := map[string]bool{}
	for _, r := range table {
		patterns[r.pattern] = true
		a, ok := routeAccess[r.pattern]
		if !ok {
			t.Errorf("%s has no entry in routeAccess, so nothing says what it requires", r.pattern)
			continue
		}
		if a.capability != r.capability {
			t.Errorf("%s requires %q in routes and %q in routeAccess", r.pattern, r.capability, a.capability)
		}
		if a.public != publicRoutes[r.pattern] {
			t.Errorf("%s is public = %v in routeAccess and %v in publicRoutes", r.pattern, a.public, publicRoutes[r.pattern])
		}
		if a.public && a.capability != "" {
			t.Errorf("%s is public and requires %q, and a request with no principal has no capabilities", r.pattern, a.capability)
		}

		method, path := splitPattern(r.pattern)
		if mutates(method) && a.capability == "" && !a.anyMember && !a.public {
			t.Errorf("%s mutates and names no capability; give it one, or say anyMember if every member may call it", r.pattern)
		}
		// {$} pins the pattern to the path itself and names nothing.
		if strings.Contains(strings.TrimSuffix(path, "{$}"), "{") {
			if a.path == "" {
				t.Errorf("%s has a wildcard and routeAccess gives no path to request it by", r.pattern)
			}
			if a.foreign == "" && !a.public {
				t.Errorf("%s names an object and routeAccess gives no foreign path, so the scope check cannot run on it", r.pattern)
			}
		}
	}

	for pattern := range routeAccess {
		if !patterns[pattern] {
			t.Errorf("routeAccess has %s and routes does not", pattern)
		}
	}
	for pattern := range publicRoutes {
		if !patterns[pattern] {
			t.Errorf("publicRoutes has %s and routes does not", pattern)
		}
	}
}

func TestRoutes_EachRouteRefusesWhatItsEntrySays(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	every := everyCapability()
	queries := routeQueries(t)

	for _, r := range routes(testLogger, testSessions(), nil, testTemplates(), testAssets()) {
		a, ok := routeAccess[r.pattern]
		if !ok {
			// The agreement test names the missing entry.
			continue
		}
		method, path := splitPattern(r.pattern)
		if a.path != "" {
			path = a.path
		}
		if method == "" {
			method = http.MethodGet
		}

		t.Run(r.pattern, func(t *testing.T) {
			resolved := 0
			handler := New(logger, testSessions(), ResolverFunc(func(context.Context, time.Time, string) (auth.Principal, error) {
				resolved++
				return auth.Principal{}, auth.ErrNoSession
			}), queries, testTemplates(), testAssets())
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))
			sentToSignIn := rec.Code == http.StatusSeeOther && rec.Header().Get("Location") == signInPath
			switch {
			case a.public && sentToSignIn:
				t.Errorf("a stranger was sent to sign in from a public route")
			case a.public && resolved > 0:
				t.Errorf("a public route cost a session lookup")
			case !a.public && !sentToSignIn:
				t.Errorf("a stranger got %d from %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, signInPath)
			}

			if a.capability != "" {
				lacking := memberWith(without(every, a.capability))
				rec = httptest.NewRecorder()
				New(logger, testSessions(), acceptEveryToken(lacking), queries, testTemplates(), testAssets()).ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), method, path, nil)))
				if rec.Code != http.StatusNotFound {
					t.Errorf("a member without %s got %d, want %d", a.capability, rec.Code, http.StatusNotFound)
				}

				rec = httptest.NewRecorder()
				New(logger, testSessions(), acceptEveryToken(memberWith(every)), queries, testTemplates(), testAssets()).ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), method, path, nil)))
				if rec.Code == http.StatusNotFound {
					t.Errorf("a member with %s got %d, so the route is hidden from the people it is for", a.capability, rec.Code)
				}
			}

			if a.foreign != "" {
				rec = httptest.NewRecorder()
				New(logger, testSessions(), acceptEveryToken(memberWith(every)), queries, testTemplates(), testAssets()).ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), method, a.foreign, nil)))
				if rec.Code != http.StatusNotFound {
					t.Errorf("an owner asking for Fairview's object at %s got %d, want %d", a.foreign, rec.Code, http.StatusNotFound)
				}
			}
		})
	}
}

// A pattern with no method matches every method, and splitPattern returns ""
// for it.
func splitPattern(pattern string) (method, path string) {
	method, path, found := strings.Cut(pattern, " ")
	if !found {
		return "", pattern
	}
	return method, path
}

func mutates(method string) bool {
	return method != http.MethodGet && method != http.MethodHead
}

// everyCapability is drawn from the route table rather than the capability
// rows because this test runs without a database. A route's gate is proven by
// removing one capability from a principal holding all the others.
func everyCapability() auth.Capabilities {
	set := auth.Capabilities{}
	for _, r := range routes(testLogger, testSessions(), nil, testTemplates(), testAssets()) {
		if r.capability != "" {
			set[r.capability] = true
		}
	}
	return set
}

func without(set auth.Capabilities, capability auth.Capability) auth.Capabilities {
	rest := make(auth.Capabilities, len(set))
	for c := range set {
		if c != capability {
			rest[c] = true
		}
	}
	return rest
}

func memberWith(capabilities auth.Capabilities) auth.Principal {
	p := sitterPrincipal()
	p.Capabilities = capabilities
	return p
}
