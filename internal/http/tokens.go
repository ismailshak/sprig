package http

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// revokeTokenPath is the URL a token row's Revoke or Remove button posts to.
func revokeTokenPath(tokenID uuid.UUID) string {
	return TokensPath + "/" + tokenID.String() + "/revoke"
}

// tokensID is both the HTML id of the page's body and the name of the
// template that renders it. Create token and every Revoke swap it.
const tokensID = "tokens"

// tokenNameMissing is the message under the name field when it is posted
// empty. The name is what tells one row from the next, so a token cannot be
// created without one.
const tokenNameMissing = "Enter a name."

// tokenLives are the lifetimes the Expires select offers, in days, shortest
// first. The ninety-day cap is the last option rather than a number to type, so
// no larger value can be entered.
var tokenLives = []int{7, 30, 60, 90}

// defaultTokenLife is the lifetime chosen when the form opens, in days.
const defaultTokenLife = 30

// tokensPage is the Tokens page: the API tokens a device sends instead of
// signing in as a person. A token can only read, so Revoke and Remove act
// without asking first.
type tokensPage struct {
	Bar topbar
	// Action is the URL the New token form posts to.
	Action string
	// Secret is the token just created. The lead paragraph explaining tokens is
	// left off the page while it is set.
	Secret *secretBox
	Tokens []tokenRow
	// AnyExpired is true when a row in the list has expired. The note under the
	// list is rendered only then.
	AnyExpired bool
	// Name is what the Name field holds, and NameError the message under it.
	Name      string
	NameError string
	// Lives is the Expires select's options.
	Lives []option
}

// tokenRow is one token in the list.
type tokenRow struct {
	Name string
	// Prefix is the start of the token, stored in the clear and shown on the
	// row. It is what tells two rows with the same name apart.
	Prefix string
	// Used reads "Last used today", or "Never used".
	Used string
	// Life is the line under the prefix and Used: "Expires 26 Oct" or "Expired
	// 31 Aug". It is on a line of its own, because a third part beside the
	// prefix truncates it.
	Life string
	// Expired is true once the date has passed. The row is greyed and stays in
	// the list, because it is still the row somebody came to remove.
	Expired bool
	// Drop is the word on the button: Revoke while the token works, and Remove
	// once it has expired.
	Drop string
	// Revoke is the URL that button posts to.
	Revoke string
}

func (h *more) tokens(w http.ResponseWriter, r *http.Request) {
	h.renderTokens(w, r, tokensPage{Lives: lifeOptions(defaultTokenLife)}, 0, "")
}

// createToken handles POST /more/tokens. It renders the token rather than
// redirecting to it, because only a hash is stored and this response is the
// one place the token itself exists.
func (h *more) createToken(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	days, err := strconv.Atoi(r.PostForm.Get("expiry"))
	if err != nil || !slices.Contains(tokenLives, days) {
		h.templates.badRequest(w, r)
		return
	}
	// TokenExpiry applies the ninety-day cap that holds for every token in the
	// app, whatever lifetime the form offered.
	expires, err := auth.TokenExpiry(h.now(), time.Duration(days)*24*time.Hour)
	if err != nil {
		h.templates.badRequest(w, r)
		return
	}

	name := strings.TrimSpace(r.PostForm.Get("name"))
	if name == "" {
		page := tokensPage{Name: name, NameError: tokenNameMissing, Lives: lifeOptions(days)}
		h.renderTokens(w, r, page, http.StatusUnprocessableEntity, tokenNameMissing)
		return
	}

	token, prefix := auth.NewAPIToken()
	params := store.CreateAPITokenParams{
		GardenID:  principal.Garden.ID,
		Name:      name,
		TokenHash: auth.HashToken(token),
		Prefix:    prefix,
		CreatedBy: principal.User.ID,
		ExpiresAt: expires,
	}
	if _, err := h.queries.CreateAPIToken(r.Context(), params); err != nil {
		h.templates.serverError(h.logger, w, r, "create the token", err)
		return
	}
	// The deadlines job sends when the token expires.
	h.wake.call()

	page := tokensPage{
		Lives: lifeOptions(defaultTokenLife),
		Secret: &secretBox{
			Label: "Your new token",
			Value: token,
			Why:   "This token is shown only once. Copy it now. It expires on " + dateWord(expires, locationFor(principal.User)) + ".",
		},
	}
	h.renderTokens(w, r, page, 0, "Token created. It’s shown only once, above the list.")
}

// revokeToken handles POST /more/tokens/{token}/revoke. Revoke and Remove are
// the same write: both set revoked_at, and the row leaves the list either
// way.
func (h *more) revokeToken(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	tokenID, err := uuid.Parse(r.PathValue("token"))
	if err != nil {
		h.templates.notFound(w, r)
		return
	}
	params := store.RevokeAPITokenParams{Now: h.now(), GardenID: principal.Garden.ID, TokenID: tokenID}
	revoked, err := h.queries.RevokeAPIToken(r.Context(), params)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "revoke the token", err)
		return
	}
	// Another garden's token and one already revoked are both 404, since
	// neither was a button this page offered.
	if revoked == 0 {
		h.templates.notFound(w, r)
		return
	}
	// The deadlines job skips a revoked token, so its next send may be later.
	h.wake.call()
	if isHTMX(r) {
		h.renderTokens(w, r, tokensPage{Lives: lifeOptions(defaultTokenLife)}, 0, "Token revoked.")
		return
	}
	http.Redirect(w, r, TokensPath, http.StatusSeeOther)
}

// renderTokens fills in the list and writes the page. A status of zero means
// 200. announce is the sentence a swap puts in the live region.
func (h *more) renderTokens(w http.ResponseWriter, r *http.Request, page tokensPage, status int, announce string) {
	principal := PrincipalFrom(r)
	tokens, err := h.queries.ListAPITokens(r.Context(), principal.Garden.ID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "list the tokens", err)
		return
	}
	page.Bar = moreBar("Tokens")
	page.Action = TokensPath
	page.Tokens = tokenRows(tokens, h.now().In(locationFor(principal.User)))
	for _, row := range page.Tokens {
		page.AnyExpired = page.AnyExpired || row.Expired
	}
	v := view{page: "tokens", status: status, announce: announce}
	if r.Header.Get("HX-Target") == tokensID {
		v.fragment = tokensID
	}
	h.templates.render(w, r, v, page)
}

// tokens holds no revoked rows, so a row that is not live is one past its
// expires_at.
func tokenRows(tokens []store.APIToken, now time.Time) []tokenRow {
	rows := make([]tokenRow, 0, len(tokens))
	for _, token := range tokens {
		expired := !auth.APITokenLive(token, now)
		row := tokenRow{
			Name:    token.Name,
			Prefix:  token.Prefix,
			Used:    usedNote(token.LastUsedAt, now),
			Expired: expired,
			Drop:    "Revoke",
			Revoke:  revokeTokenPath(token.ID),
		}
		if expired {
			row.Drop = "Remove"
			row.Life = "Expired " + agoWord(token.ExpiresAt, now)
		} else {
			row.Life = "Expires " + dateWord(token.ExpiresAt, now.Location())
		}
		rows = append(rows, row)
	}
	return rows
}

// lifeOptions builds the Expires select's options, with chosen selected.
func lifeOptions(chosen int) []option {
	options := make([]option, 0, len(tokenLives))
	for _, days := range tokenLives {
		options = append(options, option{
			Value: strconv.Itoa(days),
			Label: "In " + daysWord(days),
			On:    days == chosen,
		})
	}
	return options
}
