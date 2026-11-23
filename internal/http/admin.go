package web

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/finance"
	"github.com/x4342/marketplace/internal/moderation"
	"github.com/x4342/marketplace/internal/orders"
	"github.com/x4342/marketplace/web/templates"
)

type adminPages struct {
	identityPages
	queries  *db.Queries
	content  moderation.Service
	checkout orders.Service
}

func (p adminPages) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /disputes/{id}", p.participantDispute)
	mux.HandleFunc("POST /disputes/{id}/evidence", p.evidence)
	mux.HandleFunc("POST /disputes/{id}/appeal", p.appeal)
	mux.HandleFunc("GET /admin/disputes", p.disputes)
	mux.HandleFunc("GET /admin/disputes/{id}", p.adminDispute)
	mux.HandleFunc("POST /admin/disputes/{id}/decision", p.decideDispute)
	mux.HandleFunc("POST /listings/{id}/report", p.report)
	mux.HandleFunc("GET /account/moderation", p.myCases)
	mux.HandleFunc("POST /account/moderation/{id}/appeal", p.contentAppeal)
	mux.HandleFunc("GET /admin/content", p.contentQueue)
	mux.HandleFunc("GET /admin/content/{id}", p.contentDetail)
	mux.HandleFunc("POST /admin/content/{id}/decision", p.contentDecision)
	mux.HandleFunc("GET /admin/audit", p.audit)
}

func (p adminPages) participantDispute(w http.ResponseWriter, r *http.Request) {
	p.dispute(w, r, false)
}

func (p adminPages) adminDispute(w http.ResponseWriter, r *http.Request) {
	p.dispute(w, r, true)
}

func (p adminPages) dispute(w http.ResponseWriter, r *http.Request, admin bool) {
	actor, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	if admin && !actor.Moderator {
		p.failure(w, r, moderation.ErrRole)
		return
	}
	id := listingID(r)
	row, err := p.queries.DisputeRecord(r.Context(), id)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	participant := actor.ID == row.BuyerID || actor.ID == row.SellerID
	if !admin && !participant {
		p.failure(w, r, pgx.ErrNoRows)
		return
	}
	if admin {
		if participant {
			p.failure(w, r, moderation.ErrRole)
			return
		}
		if err := p.queries.PostAdminAudit(r.Context(), db.PostAdminAuditParams{
			ActorID: actor.ID, Area: "dispute", SubjectID: id, Action: "view", Reason: "Moderator opened the restricted dispute record",
		}); err != nil {
			p.failure(w, r, err)
			return
		}
	}
	history, err := p.queries.DisputeHistory(r.Context(), id)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	evidence, err := p.queries.CaseEvidence(r.Context(), id)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	appeals, err := p.queries.DisputeAppeals(r.Context(), id)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	p.render(w, r, 200, templates.DisputePage(templates.DisputeData{
		Record: row, History: history, Evidence: evidence, Appeals: appeals, Actor: actor.ID, Admin: admin,
		CSRF: csrfFor(w, r), Updated: r.URL.Query().Get("updated") == "1",
	}))
}

func (p adminPages) disputes(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.moderator(w, r); !ok {
		return
	}
	page := inboxPage(r)
	state := filterState(r, "open", "hold", "released", "refunded")
	rows, err := p.queries.DisputesQueue(r.Context(), db.DisputesQueueParams{State: state, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	more := len(rows) > 30
	if more {
		rows = rows[:30]
	}
	p.render(w, r, 200, templates.DisputesQueue(rows, state, page, more))
}

func (p adminPages) evidence(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	if err := p.content.Submit(r.Context(), actor.ID, id, r.PostForm.Get("kind"), r.PostForm.Get("body")); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/disputes/"+id.String()+"?updated=1", http.StatusSeeOther)
}

func (p adminPages) appeal(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	if err := p.content.DisputeAppeal(r.Context(), actor.ID, id, r.PostForm.Get("reason")); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/disputes/"+id.String()+"?updated=1", http.StatusSeeOther)
}

func (p adminPages) decideDispute(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	action, reason := r.PostForm.Get("action"), r.PostForm.Get("reason")
	var err error
	if action == "upheld" || action == "referred" {
		var appeal pgtype.UUID
		if appeal.Scan(r.PostForm.Get("appeal")) != nil {
			p.failure(w, r, moderation.ErrInvalid)
			return
		}
		err = p.content.DisputeAppealDecision(r.Context(), actor.ID, id, appeal, action, reason)
	} else {
		err = p.checkout.ResolveDispute(r.Context(), actor.ID, id, action, reason)
	}
	if err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/disputes/"+id.String()+"?updated=1", http.StatusSeeOther)
}

func (p adminPages) report(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	if err := p.content.Report(r.Context(), actor.ID, id, r.PostForm.Get("kind"), r.PostForm.Get("reason")); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/listings/"+id.String()+"?content_report=sent#content-report", http.StatusSeeOther)
}

func (p adminPages) myCases(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	page := inboxPage(r)
	rows, err := p.queries.UserContentCases(r.Context(), db.UserContentCasesParams{Actor: actor.ID, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	more := len(rows) > 30
	if more {
		rows = rows[:30]
	}
	cases := make([]templates.ContentAppealCard, 0, len(rows))
	for _, row := range rows {
		reason, err := p.queries.ContentActionReason(r.Context(), row.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			p.failure(w, r, err)
			return
		}
		cases = append(cases, templates.ContentAppealCard{Case: row, DecisionReason: reason})
	}
	p.render(w, r, 200, templates.MyContentCases(cases, csrfFor(w, r), page, more, r.URL.Query().Get("appeal") == "sent"))
}

func (p adminPages) contentAppeal(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.mutation(w, r)
	if !ok {
		return
	}
	if err := p.content.ContentAppeal(r.Context(), actor.ID, listingID(r), r.PostForm.Get("reason")); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/account/moderation?appeal=sent", http.StatusSeeOther)
}

func (p adminPages) contentQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.moderator(w, r); !ok {
		return
	}
	page := inboxPage(r)
	state := filterState(r, "open", "dismissed", "restricted", "restored", "upheld")
	rows, err := p.queries.ContentQueue(r.Context(), db.ContentQueueParams{State: state, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	more := len(rows) > 30
	if more {
		rows = rows[:30]
	}
	p.render(w, r, 200, templates.ContentQueue(rows, state, page, more))
}

func (p adminPages) contentDetail(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.moderator(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	row, err := p.queries.ContentReportRecord(r.Context(), id)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	if row.ReporterID == actor.ID || row.Affected == actor.ID || row.ConversationBuyer == actor.ID {
		p.failure(w, r, moderation.ErrRole)
		return
	}
	if err := p.queries.PostAdminAudit(r.Context(), db.PostAdminAuditParams{
		ActorID: actor.ID, Area: "content", SubjectID: id, Action: "view", Reason: "Moderator opened the restricted report",
	}); err != nil {
		p.failure(w, r, err)
		return
	}
	appeals, err := p.queries.PendingContentAppeals(r.Context(), id)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	p.render(w, r, 200, templates.ContentDetail(row, appeals, csrfFor(w, r), r.URL.Query().Get("updated") == "1"))
}

func (p adminPages) contentDecision(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	if err := p.content.ContentDecide(r.Context(), actor.ID, id, r.PostForm.Get("action"), r.PostForm.Get("reason")); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/content/"+id.String()+"?updated=1", http.StatusSeeOther)
}

func (p adminPages) audit(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	if !actor.Moderator && !actor.FinanceAdmin {
		p.failure(w, r, moderation.ErrRole)
		return
	}
	page := inboxPage(r)
	rows, err := p.queries.AdminAudit(r.Context(), db.AdminAuditParams{Content: actor.Moderator, Finance: actor.FinanceAdmin, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	more := len(rows) > 30
	if more {
		rows = rows[:30]
	}
	p.render(w, r, 200, templates.AuditPage(rows, page, more))
}

func filterState(r *http.Request, choices ...string) string {
	state := r.URL.Query().Get("state")
	if slices.Contains(choices, state) {
		return state
	}
	return ""
}

func (p adminPages) moderator(w http.ResponseWriter, r *http.Request) (db.ActiveSessionRow, bool) {
	actor, ok := p.requireSession(w, r)
	if ok && !actor.Moderator {
		p.failure(w, r, moderation.ErrRole)
		return actor, false
	}
	return actor, ok
}

func (p adminPages) mutation(w http.ResponseWriter, r *http.Request) (db.ActiveSessionRow, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil || !validCSRF(r) {
		p.denied(w, r)
		return db.ActiveSessionRow{}, false
	}
	return p.requireSession(w, r)
}

func (p adminPages) failure(w http.ResponseWriter, r *http.Request, err error) {
	status, title, detail := 503, "Case unavailable", "Try again shortly."
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		status, title, detail = 404, "Case unavailable", "Open your order or moderation page to find available cases."
	case errors.Is(err, moderation.ErrRole), errors.Is(err, finance.ErrRole):
		status, title, detail = 403, "Access denied", "A separate authorized moderator is required."
	case errors.Is(err, orders.ErrState), errors.Is(err, moderation.ErrState), errors.Is(err, finance.ErrFunds):
		status, title, detail = 409, "Case action unavailable", "Check the case state or current held balance."
	case errors.Is(err, moderation.ErrInvalid), errors.Is(err, finance.ErrInvalid):
		status, title, detail = 422, "Invalid case submission", "Select the requested option and enter text within the stated limit."
	default:
		p.log.Error("case operation failed")
	}
	p.render(w, r, status, templates.Problem(title, strings.TrimSpace(detail)))
}
