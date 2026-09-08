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

// testPushKey is a VAPID public key for the handlers that need push on. The
// subscribe route is a 404 without one.
const testPushKey = "BIKy0ljeIkYXaxRwCtYWzL6mcXZ1uQRljQ9HlisKJLJiAWkvTHx6QKQNtjbZeP5AvsVqD6-lKh6RDeyszC8lu3A"

// access is a second statement of what a route requires, kept separate from
// routes in mux.go so adding a route means deciding its access twice, and the
// test compares the two.
type access struct {
	public     bool
	capability auth.Capability
	// anyMember marks a mutating route that has no capability on purpose.
	anyMember bool
	// withoutGarden marks a route that runs for an account in no garden.
	// Every other protected route renders the "You're in no garden" page
	// instead.
	withoutGarden bool
	// path is a request path the pattern matches, using Rosewood's ids.
	path string
	// foreign is path with one of Fairview's ids in place of Rosewood's. The
	// route returns 404 for it, because no store query can find another
	// garden's row from a session on Rosewood.
	foreign string
}

var routeAccess = map[string]access{
	"GET /healthz": {public: true},
	// The hashed URL is known only at startup, so this path is a plain one the
	// pattern matches.
	assetPattern:             {public: true, path: assetPrefix + "app.css"},
	"GET /service-worker.js": {public: true},
	"GET /offline":           {public: true},
	"GET /{$}":               {},
	"GET /plants":            {},
	"GET /activity":          {},
	"GET /plants/new":        {capability: auth.PlantCreate},
	"POST /plants/new":       {capability: auth.PlantCreate},
	"GET /plants/{plant}":    {path: plantPath(rosewoodPlantID), foreign: plantPath(fairviewPlantID)},
	"GET /plants/{plant}/edit": {
		capability: auth.PlantEdit,
		path:       editPlantPath(rosewoodPlantID),
		foreign:    editPlantPath(fairviewPlantID),
	},
	"POST /plants/{plant}/edit": {
		capability: auth.PlantEdit,
		path:       editPlantPath(rosewoodPlantID),
		foreign:    editPlantPath(fairviewPlantID),
	},
	// This route uses the same plant as the edit routes, since rendering the
	// confirmation changes nothing.
	"GET /plants/{plant}/archive": {
		capability: auth.PlantArchive,
		path:       archivePlantPath(rosewoodPlantID),
		foreign:    archivePlantPath(fairviewPlantID),
	},
	"POST /plants/{plant}/archive": {
		capability: auth.PlantArchive,
		path:       archivePlantPath(rosewoodArchivedID),
		foreign:    archivePlantPath(fairviewArchivedID),
	},
	// The editor's two routes use the watering schedule, since opening the
	// editor and saving leave the schedule in place.
	"GET /plants/{plant}/schedule/{care}": {
		capability: auth.ScheduleEdit,
		path:       schedulePath(rosewoodPlantID, "water"),
		foreign:    schedulePath(fairviewPlantID, "water"),
	},
	"POST /plants/{plant}/schedule/{care}": {
		capability: auth.ScheduleEdit,
		path:       schedulePath(rosewoodPlantID, "water"),
		foreign:    schedulePath(fairviewPlantID, "water"),
	},
	// Removing uses the feeding schedule, since the request deletes it and the
	// two routes above would then have nothing to open.
	"GET /plants/{plant}/schedule/{care}/remove": {
		capability: auth.ScheduleEdit,
		path:       removeSchedulePath(rosewoodPlantID, "feed"),
		foreign:    removeSchedulePath(fairviewPlantID, "feed"),
	},
	"POST /plants/{plant}/schedule/{care}/remove": {
		capability: auth.ScheduleEdit,
		path:       removeSchedulePath(rosewoodPlantID, "feed"),
		foreign:    removeSchedulePath(fairviewPlantID, "feed"),
	},
	"GET /plants/{plant}/photos/{photo}/full": {
		path:    photoFullPath(rosewoodPlantID, rosewoodPhotoID),
		foreign: photoFullPath(fairviewPlantID, fairviewPhotoID),
	},
	"GET /plants/{plant}/photos/{photo}/square": {
		path:    photoSquarePath(rosewoodPlantID, rosewoodPhotoID),
		foreign: photoSquarePath(fairviewPlantID, fairviewPhotoID),
	},
	"GET /plants/{plant}/photos": {path: photosPath(rosewoodPlantID), foreign: photosPath(fairviewPlantID)},
	"GET /plants/{plant}/photos/new": {
		capability: auth.PhotoAdd,
		path:       newPhotoPath(rosewoodPlantID),
		foreign:    newPhotoPath(fairviewPlantID),
	},
	"POST /plants/{plant}/photos/new": {
		capability: auth.PhotoAdd,
		path:       newPhotoPath(rosewoodPlantID),
		foreign:    newPhotoPath(fairviewPlantID),
	},
	"GET /plants/{plant}/photos/{photo}": {
		path:    photoPath(rosewoodPlantID, rosewoodPhotoID),
		foreign: photoPath(fairviewPlantID, fairviewPhotoID),
	},
	// Rendering the confirmation changes nothing, so it uses the same photo as
	// the routes above.
	"GET /plants/{plant}/photos/{photo}/delete": {
		capability: auth.PhotoDeleteOwn,
		path:       deletePhotoPath(rosewoodPlantID, rosewoodPhotoID),
		foreign:    deletePhotoPath(fairviewPlantID, fairviewPhotoID),
	},
	"POST /plants/{plant}/photos/{photo}/delete": {
		capability: auth.PhotoDeleteOwn,
		path:       deletePhotoPath(rosewoodPlantID, rosewoodDeletePhotoID),
		foreign:    deletePhotoPath(fairviewPlantID, fairviewDeletePhotoID),
	},
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
	// The correcting sheet, its save and the restore share an event, since none
	// of the three changes it here: opening the sheet changes nothing, the save
	// is refused before it writes because a request with no body names no care
	// type, and the restore reaches no event at all.
	"GET /plants/{plant}/log/{event}": {
		capability: auth.CareEditOwn,
		path:       eventPath(rosewoodPlantID, rosewoodCorrectEventID, "", logQuery{}),
		foreign:    eventPath(fairviewPlantID, fairviewCorrectEventID, "", logQuery{}),
	},
	"POST /plants/{plant}/log/{event}": {
		capability: auth.CareEditOwn,
		path:       eventPath(rosewoodPlantID, rosewoodCorrectEventID, "", logQuery{}),
		foreign:    eventPath(fairviewPlantID, fairviewCorrectEventID, "", logQuery{}),
	},
	"POST /plants/{plant}/log/{event}/delete": {
		capability: auth.CareDeleteOwn,
		path:       eventPath(rosewoodPlantID, rosewoodDeleteEventID, "/delete", logQuery{}),
		foreign:    eventPath(fairviewPlantID, fairviewDeleteEventID, "/delete", logQuery{}),
	},
	// Every route under More acts on the reader's own account or their own
	// membership, so none of them names a capability. The pages a role cannot
	// use, Garden and People, are routes of their own.
	"GET /more": {},
	// Sign out runs for an account in no garden, so somebody with nothing to
	// open can still sign out.
	"POST /signout":      {anyMember: true, withoutGarden: true},
	"GET /more/account":  {},
	"POST /more/account": {anyMember: true},
	// Recovery codes belong to an account rather than to a garden, so every
	// member reaches the page.
	"GET /more/account/recovery":  {},
	"POST /more/account/recovery": {anyMember: true},
	"GET /more/passkeys":          {},
	// A passkey belongs to an account rather than to a garden, so every member
	// may add one to their own.
	"POST /more/passkeys/challenge": {anyMember: true},
	"POST /more/passkeys":           {anyMember: true},
	// The sign-in page, the challenge and the post that signs in are served
	// without a session, because signing in is what somebody with no session
	// comes to do.
	"GET /signin":            {public: true},
	"POST /signin/challenge": {public: true},
	"POST /signin":           {public: true},
	// Set up your garden is used before any session exists. The routes are
	// closed in this test, because the seeded database has accounts and sign-up
	// is off. A closed route is a 404 rather than a redirect to sign in.
	"GET /setup":            {public: true},
	"POST /setup/challenge": {public: true},
	"POST /setup":           {public: true},
	// The two routes an account that is already signed in uses to set up a
	// garden of its own. Neither names a capability, because the garden the
	// post writes is a new one of the caller's own.
	// Both run for an account in no garden. That is the account this page is
	// for.
	"GET /setup/signed-in":  {withoutGarden: true},
	"POST /setup/signed-in": {anyMember: true, withoutGarden: true},
	// An invite link is opened before any session exists. The token here was
	// never issued, so the handler renders the page for a link that cannot be
	// redeemed. There is no foreign path, because the token is what says which
	// garden the link is for.
	"GET /invite/{token}":            {public: true, path: invitedPath("no-such-token")},
	"POST /invite/{token}/challenge": {public: true, path: invitedChallengePath("no-such-token")},
	"POST /invite/{token}":           {public: true, path: invitedPath("no-such-token")},
	// A recovery code is presented before any session exists, so the page,
	// the check, the challenge and the post that saves the passkey are all
	// public. A post with no body names no code and is refused before it
	// writes.
	"GET /recover":            {public: true},
	"POST /recover":           {public: true},
	"POST /recover/challenge": {public: true},
	"POST /recover/passkey":   {public: true},
	// The garden an invite is for is the one the account is joining, so there
	// is no other garden's token to refuse. Both paths use a token nobody
	// issued. The 404 they get is the page for a link that cannot be used.
	// Both run for an account in no garden, because accepting an invite is how
	// it gets one.
	"GET /invite/{token}/accept":  {withoutGarden: true, path: acceptPath("no-such-token"), foreign: acceptPath("no-such-token")},
	"POST /invite/{token}/accept": {anyMember: true, withoutGarden: true, path: acceptPath("no-such-token"), foreign: acceptPath("no-such-token")},
	// A passkey and a push subscription belong to an account rather than to a
	// garden, so the foreign row here is another person's rather than another
	// garden's. Both routes answer 404 for one.
	"POST /more/passkeys/{key}/remove": {
		anyMember: true,
		path:      removePasskeyPath(readerPasskeyID),
		foreign:   removePasskeyPath(strangerPasskeyID),
	},
	"GET /more/notifications":  {},
	"POST /more/notifications": {anyMember: true},
	// A subscription belongs to the account, so any member may post one.
	"POST /more/notifications/browsers": {anyMember: true},
	"POST /more/notifications/browsers/{browser}/remove": {
		anyMember: true,
		path:      removeBrowserPath(readerBrowserID),
		foreign:   removeBrowserPath(strangerBrowserID),
	},
	"GET /install": {},
	// The sheet lists the reader's own memberships and the post moves their
	// own session, so every role reaches both routes. Neither has a foreign
	// path, because the garden is posted in the body rather than named in the
	// URL.
	"GET /gardens":  {},
	"POST /gardens": {anyMember: true},
	// Garden and the care types under it are the owner's pages. A care type is
	// named by slug rather than by id, so the foreign path is a slug only
	// Fairview has.
	"GET /more/garden":        {capability: auth.GardenEdit},
	"POST /more/garden":       {capability: auth.GardenEdit},
	"GET /more/garden/types":  {capability: auth.CareTypeManage},
	"POST /more/garden/types": {capability: auth.CareTypeManage},
	"GET /more/garden/types/{care}": {
		capability: auth.CareTypeManage,
		path:       careTypePath("water"),
		foreign:    careTypePath("trim"),
	},
	"POST /more/garden/types/{care}": {
		capability: auth.CareTypeManage,
		path:       careTypePath("water"),
		foreign:    careTypePath("trim"),
	},
	// Each of the three that changes a care type uses one of its own, since the
	// first to run would leave the next nothing to act on.
	"POST /more/garden/types/{care}/off": {
		capability: auth.CareTypeManage,
		path:       offCareTypePath("water"),
		foreign:    offCareTypePath("trim"),
	},
	"POST /more/garden/types/{care}/on": {
		capability: auth.CareTypeManage,
		path:       onCareTypePath("mist"),
		foreign:    onCareTypePath("trim"),
	},
	"POST /more/garden/types/{care}/delete": {
		capability: auth.CareTypeManage,
		path:       deleteCareTypePath("prune"),
		foreign:    deleteCareTypePath("trim"),
	},
	// People and the pages under it are the owner's. A member is named by
	// handle, so the foreign path is a handle only Fairview's member holds.
	"GET /more/people":        {capability: auth.MemberManage},
	"POST /more/people":       {capability: auth.MemberManage},
	"GET /more/people/invite": {capability: auth.MemberInvite},
	// A post with no body names no role, so this is refused before it writes.
	"POST /more/people/invite": {capability: auth.MemberInvite},
	"POST /more/people/invites/{invite}/revoke": {
		capability: auth.MemberManage,
		path:       revokeInvitePath(rosewoodInviteID),
		foreign:    revokeInvitePath(fairviewInviteID),
	},
	// Rendering the question changes nothing, so it shares a member with
	// Re-enrol. The post that removes one uses a member of its own, since the
	// two routes after it would find nothing left.
	"GET /more/people/{member}/remove": {
		capability: auth.MemberManage,
		path:       removeMemberPath("sam"),
		foreign:    removeMemberPath("robin"),
	},
	"POST /more/people/{member}/remove": {
		capability: auth.MemberManage,
		path:       removeMemberPath("jo"),
		foreign:    removeMemberPath("robin"),
	},
	"POST /more/people/{member}/reenrol": {
		capability: auth.MemberManage,
		path:       reenrolMemberPath("sam"),
		foreign:    reenrolMemberPath("robin"),
	},
	"GET /more/tokens": {capability: auth.TokenManage},
	// A post with no body names no lifetime, so this is refused before it
	// writes.
	"POST /more/tokens": {capability: auth.TokenManage},
	"POST /more/tokens/{token}/revoke": {
		capability: auth.TokenManage,
		path:       revokeTokenPath(rosewoodTokenID),
		foreign:    revokeTokenPath(fairviewTokenID),
	},
	// Restore puts back an event that has been deleted, so it reads no event.
	// The plant in the path is what has to be the garden's.
	"POST /plants/{plant}/log/{event}/restore": {
		capability: auth.CareDeleteOwn,
		path:       eventPath(rosewoodPlantID, rosewoodCorrectEventID, "/restore", logQuery{}),
		foreign:    eventPath(fairviewPlantID, fairviewCorrectEventID, "/restore", logQuery{}),
	},
}

var (
	fairviewID      = uuid.MustParse("00000000-0000-7000-8000-000000000201")
	rosewoodPlantID = uuid.MustParse("00000000-0000-7000-8000-000000000211")
	fairviewPlantID = uuid.MustParse("00000000-0000-7000-8000-000000000212")
	rosewoodPhotoID = uuid.MustParse("00000000-0000-7000-8000-000000000241")
	fairviewPhotoID = uuid.MustParse("00000000-0000-7000-8000-000000000242")
	// A photo uploaded with no square variant, and with no file on disk.
	rosewoodPlainPhotoID = uuid.MustParse("00000000-0000-7000-8000-000000000243")
	// The delete route removes its photo, so it gets a pair of its own.
	rosewoodDeletePhotoID = uuid.MustParse("00000000-0000-7000-8000-000000000244")
	fairviewDeletePhotoID = uuid.MustParse("00000000-0000-7000-8000-000000000245")
	rosewoodEventID       = uuid.MustParse("00000000-0000-7000-8000-000000000221")
	fairviewEventID       = uuid.MustParse("00000000-0000-7000-8000-000000000222")
	// The two routes that delete an event use one each, since the first to run
	// would leave the second a 404.
	rosewoodUndoEventID = uuid.MustParse("00000000-0000-7000-8000-000000000223")
	fairviewUndoEventID = uuid.MustParse("00000000-0000-7000-8000-000000000224")
	// The sheet's Delete needs a third pair for the same reason, and the sheet
	// itself a fourth, since the two delete routes leave the first two gone.
	rosewoodDeleteEventID  = uuid.MustParse("00000000-0000-7000-8000-000000000225")
	fairviewDeleteEventID  = uuid.MustParse("00000000-0000-7000-8000-000000000226")
	rosewoodCorrectEventID = uuid.MustParse("00000000-0000-7000-8000-000000000227")
	fairviewCorrectEventID = uuid.MustParse("00000000-0000-7000-8000-000000000228")
	// The archive route uses its own plant, since the request archives it and
	// the edit routes would then find nothing to edit.
	rosewoodArchivedID = uuid.MustParse("00000000-0000-7000-8000-000000000231")
	fairviewArchivedID = uuid.MustParse("00000000-0000-7000-8000-000000000232")
	// The stranger is a second account, holding the passkey and the push
	// subscription the More routes have to refuse. The reader holds two
	// passkeys, because the query refuses to remove the last one an account
	// has and that answer is a 404 as well.
	strangerID        = uuid.MustParse("00000000-0000-7000-8000-000000000241")
	readerPasskeyID   = uuid.MustParse("00000000-0000-7000-8000-000000000242")
	strangerPasskeyID = uuid.MustParse("00000000-0000-7000-8000-000000000243")
	readerBrowserID   = uuid.MustParse("00000000-0000-7000-8000-000000000244")
	strangerBrowserID = uuid.MustParse("00000000-0000-7000-8000-000000000245")
	secondPasskeyID   = uuid.MustParse("00000000-0000-7000-8000-000000000246")
	// The rows People and Tokens act on. Sam and Jo are in Rosewood and Robin
	// is in Fairview, so the handle in a foreign path is one that exists and
	// the reader's garden does not hold.
	joID             = uuid.MustParse("00000000-0000-7000-8000-000000000247")
	fairviewMemberID = uuid.MustParse("00000000-0000-7000-8000-000000000248")
	rosewoodInviteID = uuid.MustParse("00000000-0000-7000-8000-000000000251")
	fairviewInviteID = uuid.MustParse("00000000-0000-7000-8000-000000000252")
	rosewoodTokenID  = uuid.MustParse("00000000-0000-7000-8000-000000000253")
	fairviewTokenID  = uuid.MustParse("00000000-0000-7000-8000-000000000254")
)

// routeQueries seeds two gardens, each with a plant scheduled for both its care
// types and two care events, because a route naming an object needs one in
// sitterPrincipal's garden and one outside it. The events are the principal's
// own because the delete route is tested with a caller who may delete only
// their own. The second care type's schedule is the one the remove route
// deletes.
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
		{`INSERT INTO care_type (garden_id, name, slug)
			VALUES ($1, 'Water', 'water'), ($2, 'Water', 'water'), ($1, 'Feed', 'feed'), ($2, 'Feed', 'feed')`, []any{rosewoodID, fairviewID}},
		// The Garden page's routes: a type with no events for the delete route,
		// one that is already off for the route that turns one back on, and one
		// only Fairview has for the scope check.
		{`INSERT INTO care_type (garden_id, name, slug, archived_at)
			VALUES ($1, 'Prune', 'prune', NULL), ($1, 'Mist', 'mist', now()), ($2, 'Trim', 'trim', NULL)`, []any{rosewoodID, fairviewID}},
		{"INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Big Fella'), ($3, $4, 'Gerald')", []any{rosewoodPlantID, rosewoodID, fairviewPlantID, fairviewID}},
		{"INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Doris'), ($3, $4, 'Nigel')", []any{rosewoodArchivedID, rosewoodID, fairviewArchivedID, fairviewID}},
		{`INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit)
			SELECT garden_id, $1, id, 7, 'day' FROM care_type WHERE garden_id = $2`, []any{rosewoodPlantID, rosewoodID}},
		{`INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit)
			SELECT garden_id, $1, id, 7, 'day' FROM care_type WHERE garden_id = $2`, []any{fairviewPlantID, fairviewID}},
		{"INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Ellie', 'ellie', 'Europe/London')", []any{sitterPrincipal().User.ID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4 AND slug = 'water'`, []any{rosewoodEventID, rosewoodPlantID, sitterPrincipal().User.ID, rosewoodID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4 AND slug = 'water'`, []any{fairviewEventID, fairviewPlantID, sitterPrincipal().User.ID, fairviewID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4 AND slug = 'water'`, []any{rosewoodUndoEventID, rosewoodPlantID, sitterPrincipal().User.ID, rosewoodID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4 AND slug = 'water'`, []any{fairviewUndoEventID, fairviewPlantID, sitterPrincipal().User.ID, fairviewID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4 AND slug = 'water'`, []any{rosewoodDeleteEventID, rosewoodPlantID, sitterPrincipal().User.ID, rosewoodID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4 AND slug = 'water'`, []any{fairviewDeleteEventID, fairviewPlantID, sitterPrincipal().User.ID, fairviewID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4 AND slug = 'water'`, []any{rosewoodCorrectEventID, rosewoodPlantID, sitterPrincipal().User.ID, rosewoodID}},
		{`INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, garden_id, $2, id, $3, now(), true FROM care_type WHERE garden_id = $4 AND slug = 'water'`, []any{fairviewCorrectEventID, fairviewPlantID, sitterPrincipal().User.ID, fairviewID}},
		// The rows the routes under More read: a second account, and passkeys
		// and push subscriptions on both accounts.
		{"INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Sam', 'sam', 'Europe/London')", []any{strangerID}},
		{`INSERT INTO passkey_credential (id, user_id, credential_id, name, public_key)
			VALUES ($1, $2, 'reader-one', 'iPhone', '\x00'), ($3, $2, 'reader-two', 'MacBook Air', '\x00'), ($4, $5, 'stranger-one', 'iPhone', '\x00')`,
			[]any{readerPasskeyID, sitterPrincipal().User.ID, secondPasskeyID, strangerPasskeyID, strangerID}},
		{`INSERT INTO push_subscription (id, user_id, endpoint, p256dh_key, auth_key)
			VALUES ($1, $2, 'https://push.invalid/reader', 'key', 'key'), ($3, $4, 'https://push.invalid/stranger', 'key', 'key')`,
			[]any{readerBrowserID, sitterPrincipal().User.ID, strangerBrowserID, strangerID}},
		// The rows People and Tokens act on. The reader is a member of
		// Rosewood, because a page listing members has to find their own row.
		{"INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Jo', 'jo', 'Europe/London'), ($2, 'Robin', 'robin', 'Europe/Lisbon')", []any{joID, fairviewMemberID}},
		{`INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'owner', 8), ($1, $3, 'member', 8), ($1, $4, 'sitter', 8), ($5, $6, 'member', 8)`,
			[]any{rosewoodID, sitterPrincipal().User.ID, strangerID, joID, fairviewID, fairviewMemberID}},
		{`INSERT INTO invite (id, garden_id, token_hash, role, created_by, expires_at)
			VALUES ($1, $2, 'rosewood-invite', 'sitter', $3, now() + interval '7 days'),
			       ($4, $5, 'fairview-invite', 'sitter', $6, now() + interval '7 days')`,
			[]any{rosewoodInviteID, rosewoodID, sitterPrincipal().User.ID, fairviewInviteID, fairviewID, fairviewMemberID}},
		{`INSERT INTO api_token (id, garden_id, name, token_hash, prefix, created_by, expires_at)
			VALUES ($1, $2, 'The kitchen display', 'rosewood-token', 'sprg_1111', $3, now() + interval '30 days'),
			       ($4, $5, 'The kitchen display', 'fairview-token', 'sprg_2222', $6, now() + interval '30 days')`,
			[]any{rosewoodTokenID, rosewoodID, sitterPrincipal().User.ID, fairviewTokenID, fairviewID, fairviewMemberID}},
		// A photo on each garden's plant, a third on Rosewood with no square
		// variant, and a pair for the delete route to remove.
		{`INSERT INTO photo (id, garden_id, plant_id, uploaded_by, kind, path, width, height, bytes, square_bytes)
			VALUES ($1, $2, $3, $4, 'image/jpeg', 'rosewood.jpg', 3, 2, 100, 40),
			       ($5, $6, $7, $8, 'image/jpeg', 'fairview.jpg', 3, 2, 100, 40),
			       ($9, $2, $3, $4, 'image/jpeg', 'rosewood-plain.jpg', 3, 2, 100, NULL),
			       ($10, $2, $3, $4, 'image/jpeg', 'rosewood-delete.jpg', 3, 2, 100, 40),
			       ($11, $6, $7, $8, 'image/jpeg', 'fairview-delete.jpg', 3, 2, 100, 40)`,
			[]any{rosewoodPhotoID, rosewoodID, rosewoodPlantID, sitterPrincipal().User.ID, fairviewPhotoID, fairviewID, fairviewPlantID, fairviewMemberID, rosewoodPlainPhotoID, rosewoodDeletePhotoID, fairviewDeletePhotoID}},
	}

	for _, row := range seed {
		if _, err := tx.Exec(ctx, row.sql, row.args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, row.sql)
		}
	}
	return store.New(tx)
}

func TestRoutes_EveryRouteHasOneRouteAccessEntryThatMatchesIt(t *testing.T) {
	table := routes(testLogger, testSessions(), testPasskeys(), nil, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil)
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
		if a.withoutGarden != r.withoutGarden {
			t.Errorf("%s is served without a garden = %v in routeAccess and %v in routes", r.pattern, a.withoutGarden, r.withoutGarden)
		}
		if a.withoutGarden && (a.public || a.capability != "") {
			t.Errorf("%s is served without a garden and is public or requires %q, and neither goes with a session on no garden", r.pattern, a.capability)
		}

		method, path := splitPattern(r.pattern)
		if mutates(method) && a.capability == "" && !a.anyMember && !a.public {
			t.Errorf("%s mutates and names no capability; give it one, or say anyMember if every member may call it", r.pattern)
		}
		// {$} anchors the pattern to the exact path and is not a wildcard.
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

func TestRoutes_EachRouteRefusesStrangersAndRolesAsItsEntrySays(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	every := everyCapability()
	queries := routeQueries(t)

	for _, r := range routes(testLogger, testSessions(), testPasskeys(), nil, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil) {
		a, ok := routeAccess[r.pattern]
		if !ok {
			// The test above reports the missing entry.
			continue
		}
		method, path := splitPattern(r.pattern)
		if a.path != "" {
			path = a.path
		}
		// {$} anchors a pattern to the exact path. It is not part of the path
		// a request is made to.
		path = strings.TrimSuffix(path, "{$}")
		if method == "" {
			method = http.MethodGet
		}

		t.Run(r.pattern, func(t *testing.T) {
			resolved := 0
			handler := New(logger, testSessions(), testPasskeys(), ResolverFunc(func(context.Context, time.Time, string) (auth.Principal, error) {
				resolved++
				return auth.Principal{}, auth.ErrNoSession
			}), queries, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil)
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
				New(logger, testSessions(), testPasskeys(), acceptEveryToken(lacking), queries, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil).ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), method, path, nil)))
				if rec.Code != http.StatusNotFound {
					t.Errorf("a member without %s got %d, want %d", a.capability, rec.Code, http.StatusNotFound)
				}

				rec = httptest.NewRecorder()
				New(logger, testSessions(), testPasskeys(), acceptEveryToken(memberWith(every)), queries, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil).ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), method, path, nil)))
				if rec.Code == http.StatusNotFound {
					t.Errorf("a member with %s got %d, so the route is hidden from the people it is for", a.capability, rec.Code)
				}
			}

			if !a.public {
				rec = httptest.NewRecorder()
				New(logger, testSessions(), testPasskeys(), acceptEveryToken(noGardenPrincipal()), queries, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil).ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), method, path, nil)))
				gotPage := rec.Code == http.StatusOK && onNoGardenPage(rec.Body.String())
				if a.withoutGarden && gotPage {
					t.Errorf("a session on no garden got the no-garden page from a route that is served without one")
				}
				if !a.withoutGarden && !gotPage {
					t.Errorf("a session on no garden got %d from a route that needs one, want the no-garden page", rec.Code)
				}
			}

			if a.foreign != "" {
				rec = httptest.NewRecorder()
				New(logger, testSessions(), testPasskeys(), acceptEveryToken(memberWith(every)), queries, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil).ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), method, a.foreign, nil)))
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

// everyCapability is collected from the route table rather than the database,
// because this test runs without one. A route's check is proven by removing one
// capability from a principal holding all the others.
func everyCapability() auth.Capabilities {
	set := auth.Capabilities{}
	for _, r := range routes(testLogger, testSessions(), testPasskeys(), nil, nil, testTemplates(), testAssets(), "", false, testPushKey, nil, nil) {
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
