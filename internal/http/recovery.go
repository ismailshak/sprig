package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// recoveryPage is the Recovery codes page, reached from Account.
type recoveryPage struct {
	Bar topbar
	// Action is the URL the Create codes button posts to.
	Action string
	// Codes is a batch in plaintext. Only hashes are stored, so it is set on
	// the single response that created the batch and is empty on every other
	// request.
	Codes []string
	// Live is true when the account has recovery codes.
	Live bool
	// Left reads "8 of 10 left".
	Left string
	// Made reads "Made 2 Aug".
	Made string
	// Prompted shows the paragraph saying only an owner is prompted for codes.
	// It is true for a person who manages the garden's people. Everyone else
	// can still reach this page and make codes.
	Prompted bool
}

// recoveryBar is the top bar on the Recovery codes page. It goes back to
// Account rather than to More, because the page is reached from Account.
func recoveryBar() topbar {
	return topbar{Href: accountPath, Back: "Account", Title: "Recovery codes"}
}

func (h *more) recovery(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	batch, live, err := h.recoveryBatch(r.Context(), principal.User.ID)
	if err != nil {
		serverError(h.logger, w, r, "open the recovery codes", err)
		return
	}
	page := recoveryPage{
		Bar:      recoveryBar(),
		Action:   recoveryPath,
		Live:     live,
		Prompted: principal.Can(auth.MemberManage),
	}
	if live {
		page.Left = codesLeftWord(batch.Unused, batch.Size)
		page.Made = "Made " + agoWord(batch.MadeAt, h.now().In(locationFor(principal.User)))
	}
	h.templates.render(w, r, view{page: "recovery"}, page)
}

// recoveryBatch reads an account's recovery codes. The bool is false when
// there are none. That is not an error: the query returns no rows for an
// account that has never made a set.
func (h *more) recoveryBatch(ctx context.Context, userID uuid.UUID) (store.GetRecoveryBatchRow, bool, error) {
	batch, err := h.queries.GetRecoveryBatch(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.GetRecoveryBatchRow{}, false, nil
	}
	if err != nil {
		return store.GetRecoveryBatchRow{}, false, fmt.Errorf("read the recovery codes: %w", err)
	}
	return batch, true, nil
}

// createCodes handles POST /more/account/recovery. It replaces whatever codes
// the account holds with a batch of ten and renders the page with the new
// codes in plaintext. The delete and the insert are one transaction, so an
// account is never left holding two batches or none at all.
func (h *more) createCodes(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	codes := make([]string, 0, auth.RecoveryBatchSize)
	hashes := make([]string, 0, auth.RecoveryBatchSize)
	for range auth.RecoveryBatchSize {
		code := auth.NewRecoveryCode()
		codes = append(codes, code)
		hashes = append(hashes, auth.HashToken(code))
	}
	err := h.queries.InTx(r.Context(), func(q *store.Queries) error {
		if err := q.DeleteRecoveryCodes(r.Context(), principal.User.ID); err != nil {
			return err
		}
		return q.CreateRecoveryBatch(r.Context(), store.CreateRecoveryBatchParams{
			UserID:      principal.User.ID,
			CodeHashes:  hashes,
			GeneratedAt: h.now(),
		})
	})
	if err != nil {
		serverError(h.logger, w, r, "create the recovery codes", err)
		return
	}
	h.templates.render(w, r, view{page: "recovery"}, recoveryPage{Bar: recoveryBar(), Codes: codes})
}
