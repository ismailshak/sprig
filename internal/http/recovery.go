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

func (h *more) recovery(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	batch, live, err := h.recoveryBatch(r.Context(), principal.User.ID)
	if err != nil {
		serverError(h.logger, w, r, "open the recovery codes", err)
		return
	}
	page := recoveryPage{Live: live, Prompted: principal.Can(auth.MemberManage)}
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
