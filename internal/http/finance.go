package web

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/x4342/marketplace/internal/authz"
	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/finance"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/internal/orders"
	"github.com/x4342/marketplace/web/templates"
)

type financePages struct {
	identityPages
	queries  *db.Queries
	ledger   finance.Service
	checkout orders.Service
}

func (p financePages) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /earnings", p.earnings)
	mux.HandleFunc("POST /payouts", p.request)
	mux.HandleFunc("GET /payouts/{id}", p.sellerDetail)
	mux.HandleFunc("GET /admin/payouts", p.queue)
	mux.HandleFunc("GET /admin/payouts/{id}", p.adminDetail)
	mux.HandleFunc("POST /admin/payouts/{id}/action", p.adminAction)
	mux.HandleFunc("GET /admin/payment-adjustments", p.adjustments)
	mux.HandleFunc("POST /admin/payment-adjustments/{id}", p.adjust)
}

func (p financePages) earnings(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	p.renderEarnings(w, r, actor, 200, "", "", "", "")
}

func (p financePages) renderEarnings(w http.ResponseWriter, r *http.Request, actor db.ActiveSessionRow, status int, problem, code, amount, key string) {
	balances, err := p.queries.SellerBalances(r.Context(), actor.ID)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	page := inboxPage(r)
	payouts, err := p.queries.SellerPayouts(r.Context(), db.SellerPayoutsParams{SellerID: actor.ID, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	more := len(payouts) > 30
	if more {
		payouts = payouts[:30]
	}
	if key == "" {
		key = identity.NewToken()
	}
	p.render(w, r, status, templates.Earnings(templates.EarningsData{
		Balances: balances, Payouts: payouts, CSRF: csrfFor(w, r), Key: key, Page: page, More: more, Problem: problem, Currency: code, Amount: amount,
	}))
}

func (p financePages) request(w http.ResponseWriter, r *http.Request) {
	if !validForm(w, r) {
		p.denied(w, r)
		return
	}
	actor, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	code, amount, key := r.PostForm.Get("currency"), r.PostForm.Get("amount"), r.PostForm.Get("key")
	if !identity.ValidToken(key) || len(r.PostForm["key"]) != 1 {
		p.denied(w, r)
		return
	}
	cents, err := finance.ParseAmount(amount)
	if err != nil {
		p.renderEarnings(w, r, actor, 422, "Enter a positive amount with at most two decimal places.", code, amount, key)
		return
	}
	id, err := p.ledger.Request(r.Context(), actor.ID, code, cents, key, r.PostForm.Get("scenario"))
	switch {
	case errors.Is(err, finance.ErrFunds):
		p.renderEarnings(w, r, actor, 409, "Available earnings cannot cover this request.", code, amount, key)
	case errors.Is(err, finance.ErrInvalid):
		p.renderEarnings(w, r, actor, 422, "Choose USD or EUR and a supported simulation scenario.", code, amount, key)
	case err != nil:
		p.failure(w, r, err)
	default:
		http.Redirect(w, r, "/payouts/"+id.String()+"?created=1", http.StatusSeeOther)
	}
}

func (p financePages) sellerDetail(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	row, err := p.queries.SellerPayout(r.Context(), db.SellerPayoutParams{ID: listingID(r), SellerID: actor.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	p.detail(w, r, templates.PayoutData{ID: row.ID, State: row.State, Currency: row.Currency, Amount: row.AmountMinor, Units: row.TestUnits,
		Attempts: row.Attempts, HoldActive: row.HoldActive, Updated: r.URL.Query().Get("created") == "1"})
}

func (p financePages) queue(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.admin(w, r); !ok {
		return
	}
	page := inboxPage(r)
	state := filterState(r, "reserved", "under_review", "approved", "denied", "processing", "confirmed", "reconciled", "failed", "stalled")
	rows, err := p.queries.FinancePayoutQueue(r.Context(), db.FinancePayoutQueueParams{State: state, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	more := len(rows) > 30
	if more {
		rows = rows[:30]
	}
	p.render(w, r, 200, templates.FinancePayoutQueue(rows, state, page, more))
}

func (p financePages) adminDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.admin(w, r); !ok {
		return
	}
	row, err := p.queries.FinancePayout(r.Context(), listingID(r))
	if err != nil {
		p.failure(w, r, err)
		return
	}
	p.detail(w, r, templates.PayoutData{ID: row.ID, State: row.State, Currency: row.Currency, Amount: row.AmountMinor, Units: row.TestUnits,
		Attempts: row.Attempts, HoldActive: row.HoldActive, Admin: true, Seller: row.Username, Available: row.Available, Held: row.Held,
		CSRF: csrfFor(w, r), Updated: r.URL.Query().Get("updated") == "1"})
}

func (p financePages) detail(w http.ResponseWriter, r *http.Request, data templates.PayoutData) {
	history, err := p.queries.PayoutActivity(r.Context(), data.ID)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	data.History = history
	reconciliation, err := p.queries.PayoutReconciliation(r.Context(), data.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		p.failure(w, r, err)
		return
	}
	data.Reconciliation = reconciliation
	p.render(w, r, 200, templates.PayoutDetail(data))
}

func (p financePages) adminAction(w http.ResponseWriter, r *http.Request) {
	if !validForm(w, r) {
		p.denied(w, r)
		return
	}
	actor, ok := p.admin(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	if !p.requireFreshStaff(w, r, actor) {
		return
	}
	if err := p.ledger.Act(r.Context(), actor.ID, id, r.PostForm.Get("action"), r.PostForm.Get("reason")); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/payouts/"+id.String()+"?updated=1", http.StatusSeeOther)
}

func (p financePages) adjustments(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.admin(w, r); !ok {
		return
	}
	page := inboxPage(r)
	rows, err := p.queries.FinanceOrderQueue(r.Context(), 30*(page-1))
	if err != nil {
		p.failure(w, r, err)
		return
	}
	more := len(rows) > 30
	if more {
		rows = rows[:30]
	}
	p.render(w, r, 200, templates.PaymentAdjustments(rows, csrfFor(w, r), page, more, r.URL.Query().Get("updated") == "1"))
}

func (p financePages) adjust(w http.ResponseWriter, r *http.Request) {
	if !validForm(w, r) {
		p.denied(w, r)
		return
	}
	actor, ok := p.admin(w, r)
	if !ok {
		return
	}
	if !p.requireFreshStaff(w, r, actor) {
		return
	}
	if err := p.checkout.Adjust(r.Context(), actor.ID, listingID(r), r.PostForm.Get("action"), r.PostForm.Get("reason")); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/payment-adjustments?updated=1", http.StatusSeeOther)
}

func (p financePages) admin(w http.ResponseWriter, r *http.Request) (db.ActiveSessionRow, bool) {
	actor, ok := p.requireSession(w, r)
	if ok && !authz.Allows(actor.Role, authz.Staff) {
		p.failure(w, r, finance.ErrRole)
		return actor, false
	}
	return actor, ok
}

func (p financePages) failure(w http.ResponseWriter, r *http.Request, err error) {
	status, title, detail := 503, "Finance unavailable", "Try again shortly."
	switch {
	case errors.Is(err, finance.ErrRole):
		status, title, detail = 403, "Access denied", "This operation requires a moderator or administrator who is independent of the transaction."
	case errors.Is(err, pgx.ErrNoRows):
		status, title, detail = 404, "Finance record unavailable", "Open your earnings or the finance queue."
	case errors.Is(err, finance.ErrFunds):
		status, title, detail = 409, "Insufficient funds", "The available balance cannot cover this action. The journal and reserved funds are unchanged."
	case errors.Is(err, finance.ErrState):
		status, title, detail = 409, "Finance action unavailable", "Check the current record state and refresh before trying again."
	case errors.Is(err, finance.ErrInvalid):
		status, title, detail = 422, "Invalid finance action", "Choose a supported action and provide a reason up to 1,000 characters."
	default:
		p.log.Error("finance operation failed")
	}
	p.render(w, r, status, templates.Problem(title, detail))
}
