package web

import (
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/web/templates"
)

var (
	errThreadBlocked  = errors.New("conversation blocked")
	errMessageLimited = errors.New("messaging rate limit")
)

type messagePages struct {
	identityPages
	queries *db.Queries
	pool    *pgxpool.Pool
}

func (p messagePages) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /messages", p.workspace)
	mux.HandleFunc("POST /messages/direct", p.openDirect)
	mux.HandleFunc("GET /messages/{id}", p.workspace)
	mux.HandleFunc("GET /messages/{id}/entries", p.fragment)
	mux.HandleFunc("POST /listings/{id}/conversation", p.open)
	mux.HandleFunc("POST /messages/{id}/send", p.send)
	mux.HandleFunc("POST /messages/{id}/read", p.markRead)
	mux.HandleFunc("POST /messages/{id}/block", p.block)
	mux.HandleFunc("POST /messages/{id}/report", p.report)
	mux.HandleFunc("GET /notifications", p.notifications)
	mux.HandleFunc("GET /notifications/{id}", p.notification)
}

func (p messagePages) workspace(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	if r.PathValue("id") == "" {
		var err error
		id, err = p.queries.LatestConversation(r.Context(), session.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			p.failure(w, r, err)
			return
		}
	} else if !id.Valid {
		p.failure(w, r, pgx.ErrNoRows)
		return
	}
	var thread templates.ThreadData
	if id.Valid {
		var loaded bool
		thread, loaded = p.loadThread(w, r, session.ID, id)
		if !loaded {
			return
		}
	}
	p.renderWorkspace(w, r, session.ID, thread, http.StatusOK)
}

func (p messagePages) renderWorkspace(w http.ResponseWriter, r *http.Request, actor pgtype.UUID, thread templates.ThreadData, status int) {
	page := inboxPage(r)
	rows, err := p.queries.Inbox(r.Context(), db.InboxParams{Actor: actor, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	more := len(rows) > 30
	if more {
		rows = rows[:30]
	}
	p.render(w, r, status, templates.MessageWorkspace(rows, thread, page, more, r.URL.Query().Get("updated")))
}

func (p messagePages) openDirect(w http.ResponseWriter, r *http.Request) {
	session, ok := p.mutation(w, r)
	if !ok {
		return
	}
	username := identity.NormalizeUsername(r.PostForm.Get("username"))
	if !identity.ValidUsername(username) {
		p.failure(w, r, pgx.ErrNoRows)
		return
	}
	ctx := r.Context()
	var id pgtype.UUID
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		if err := q.LockMessagingActor(ctx, session.ID.String()); err != nil {
			return err
		}
		peer, err := q.DirectPeer(ctx, db.DirectPeerParams{Username: username, Actor: session.ID})
		if err != nil {
			return err
		}
		id, err = q.DirectConversation(ctx, db.DirectConversationParams{Actor: session.ID, Peer: peer})
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		limited, err := q.DirectOpenLimited(ctx, session.ID)
		if err != nil {
			return err
		}
		if limited {
			return errMessageLimited
		}
		id, err = q.CreateDirectConversation(ctx, db.CreateDirectConversationParams{Actor: session.ID, Peer: peer})
		return err
	})
	if errors.Is(err, errMessageLimited) {
		w.Header().Set("Retry-After", "60")
		p.problem(w, r, http.StatusTooManyRequests, "Try again shortly", "Wait a minute before starting another conversation.")
		return
	}
	if err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, messageURL(id), http.StatusSeeOther)
}

func (p messagePages) open(w http.ResponseWriter, r *http.Request) {
	session, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id, err := p.queries.OpenConversation(r.Context(), db.OpenConversationParams{ListingID: listingID(r), BuyerID: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, messageURL(id), http.StatusSeeOther)
}

func (p messagePages) loadThread(w http.ResponseWriter, r *http.Request, actor, id pgtype.UUID) (templates.ThreadData, bool) {
	thread, err := p.queries.Thread(r.Context(), db.ThreadParams{ThreadID: id, Actor: actor})
	if err != nil {
		p.failure(w, r, err)
		return templates.ThreadData{}, false
	}
	before := int64(math.MaxInt64)
	if number, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil && number > 0 {
		before = number
	}
	rows, err := p.queries.ThreadMessages(r.Context(), db.ThreadMessagesParams{ThreadID: id, Actor: actor, BeforeSequence: before})
	if err != nil {
		p.failure(w, r, err)
		return templates.ThreadData{}, false
	}
	data := templates.ThreadData{Thread: thread, CSRF: csrfFor(w, r), Latest: before == math.MaxInt64}
	if len(rows) > 30 {
		rows = rows[:30]
		data.Older = messageURL(id) + "?before=" + strconv.FormatInt(rows[len(rows)-1].Sequence, 10)
	}
	if len(rows) > 0 {
		data.LatestSequence = rows[0].Sequence
	}
	slices.Reverse(rows)
	data.Messages = rows
	if data.Latest {
		data.Events, err = p.queries.ThreadEvents(r.Context(), db.ThreadEventsParams{ThreadID: id, Actor: actor})
		if err != nil {
			p.failure(w, r, err)
			return templates.ThreadData{}, false
		}
	}
	return data, true
}

func (p messagePages) fragment(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	if data, loaded := p.loadThread(w, r, session.ID, listingID(r)); loaded {
		p.render(w, r, http.StatusOK, templates.ThreadEntries(data))
	}
}

func (p messagePages) send(w http.ResponseWriter, r *http.Request) {
	session, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	body := strings.TrimSpace(r.PostForm.Get("message"))
	if !validText(body, 2000) {
		p.sendFailure(w, r, session.ID, id, body, http.StatusUnprocessableEntity, "Enter a message between 1 and 2,000 characters.")
		return
	}
	err := pgx.BeginFunc(r.Context(), p.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		if err := q.LockMessagingActor(r.Context(), session.ID.String()); err != nil {
			return err
		}
		participants, err := q.LockConversation(r.Context(), db.LockConversationParams{ThreadID: id, Actor: session.ID})
		if err != nil {
			return err
		}
		thread, err := q.Thread(r.Context(), db.ThreadParams{ThreadID: id, Actor: session.ID})
		if err != nil {
			return err
		}
		if thread.Blocked {
			return errThreadBlocked
		}
		limited, err := q.SendMessageLimited(r.Context(), session.ID)
		if err != nil {
			return err
		}
		if limited {
			return errMessageLimited
		}
		message, err := q.AddMessage(r.Context(), db.AddMessageParams{ConversationID: id, SenderID: session.ID, Body: body})
		if err != nil {
			return err
		}
		other := participants.SellerID
		if other == session.ID {
			other = participants.BuyerID
		}
		if err := q.AddMessageNotification(r.Context(), db.AddMessageNotificationParams{UserID: other, ConversationID: id, MessageID: message}); err != nil {
			return err
		}
		return q.TouchConversation(r.Context(), id)
	})
	if errors.Is(err, errThreadBlocked) {
		p.sendFailure(w, r, session.ID, id, body, http.StatusForbidden, "New messages are paused in this conversation.")
		return
	}
	if errors.Is(err, errMessageLimited) {
		w.Header().Set("Retry-After", "60")
		p.sendFailure(w, r, session.ID, id, body, http.StatusTooManyRequests, "Wait a minute before sending another message.")
		return
	}
	if err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, messageURL(id)+"?updated=sent", http.StatusSeeOther)
}

func (p messagePages) sendFailure(w http.ResponseWriter, r *http.Request, actor, id pgtype.UUID, draft string, status int, problem string) {
	if thread, ok := p.loadThread(w, r, actor, id); ok {
		thread.Draft, thread.Problem = draft, problem
		p.renderWorkspace(w, r, actor, thread, status)
	}
}

func (p messagePages) block(w http.ResponseWriter, r *http.Request) {
	session, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	action := r.PostForm.Get("block")
	if action != "block" && action != "unblock" {
		p.denied(w, r)
		return
	}
	err := pgx.BeginFunc(r.Context(), p.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		if _, err := q.LockConversation(r.Context(), db.LockConversationParams{ThreadID: id, Actor: session.ID}); err != nil {
			return err
		}
		if action == "unblock" {
			return q.UnblockConversation(r.Context(), db.UnblockConversationParams{ConversationID: id, UserID: session.ID})
		}
		return q.BlockConversation(r.Context(), db.BlockConversationParams{ConversationID: id, UserID: session.ID})
	})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, messageURL(id)+"?updated=block", http.StatusSeeOther)
}

func (p messagePages) markRead(w http.ResponseWriter, r *http.Request) {
	session, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	seq, err := strconv.ParseInt(r.PostForm.Get("sequence"), 10, 64)
	if err != nil || seq < 1 {
		p.denied(w, r)
		return
	}
	if _, err := p.queries.Thread(r.Context(), db.ThreadParams{ThreadID: id, Actor: session.ID}); err != nil {
		p.failure(w, r, err)
		return
	}
	if err := p.queries.AcknowledgeConversation(r.Context(), db.AcknowledgeConversationParams{ThreadID: id, Actor: session.ID, LastSequence: seq}); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, messageURL(id), http.StatusSeeOther)
}

func (p messagePages) report(w http.ResponseWriter, r *http.Request) {
	session, ok := p.mutation(w, r)
	if !ok {
		return
	}
	id := listingID(r)
	reason := strings.TrimSpace(r.PostForm.Get("reason"))
	if !validText(reason, 1000) {
		p.problem(w, r, http.StatusUnprocessableEntity, "Report could not be sent", "Enter a reason between 1 and 1,000 characters.")
		return
	}
	if _, err := p.queries.Thread(r.Context(), db.ThreadParams{ThreadID: id, Actor: session.ID}); err != nil {
		p.failure(w, r, err)
		return
	}
	if err := p.queries.ReportConversation(r.Context(), db.ReportConversationParams{ThreadID: id, Actor: session.ID, Reason: reason}); err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, messageURL(id)+"?updated=reported", http.StatusSeeOther)
}

func (p messagePages) notifications(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	page := inboxPage(r)
	rows, err := p.queries.MessageNotifications(r.Context(), db.MessageNotificationsParams{Actor: session.ID, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	orderNotices, err := p.queries.UserOrderNotifications(r.Context(), db.UserOrderNotificationsParams{Actor: session.ID, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	reviewNotices, err := p.queries.ReviewNotices(r.Context(), db.ReviewNoticesParams{Actor: session.ID, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	payoutNotices, err := p.queries.UserPayoutNotices(r.Context(), db.UserPayoutNoticesParams{SellerID: session.ID, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	disputeNotices, err := p.queries.UserDisputeNotices(r.Context(), db.UserDisputeNoticesParams{Actor: session.ID, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, templates.Notifications(rows, orderNotices, reviewNotices, payoutNotices, disputeNotices, page))
}

func (p messagePages) notification(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	notif, err := p.queries.MessageNotificationDestination(r.Context(), db.MessageNotificationDestinationParams{NotificationID: listingID(r), Actor: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	dest := messageURL(notif.ConversationID) + "?before=" + strconv.FormatInt(notif.Sequence+1, 10) + "#message-" + notif.MessageID.String()
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (p messagePages) mutation(w http.ResponseWriter, r *http.Request) (db.ActiveSessionRow, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil || !validCSRF(r) {
		p.denied(w, r)
		return db.ActiveSessionRow{}, false
	}
	return p.requireSession(w, r)
}

func (p messagePages) failure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		p.problem(w, r, http.StatusNotFound, "Conversation unavailable", "Open Messages to find your conversations.")
		return
	}
	p.log.Error("messaging operation failed")
	p.problem(w, r, http.StatusServiceUnavailable, "Messages unavailable", "Try again shortly.")
}

func (p messagePages) problem(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	p.render(w, r, status, templates.Problem(title, message))
}

func validText(text string, limit int) bool {
	n := utf8.RuneCountInString(text)
	return utf8.ValidString(text) && n > 0 && n <= limit && !strings.ContainsRune(text, 0)
}

func inboxPage(r *http.Request) int32 {
	return inboxPageFor(r, "page")
}

func inboxPageFor(r *http.Request, key string) int32 {
	page, err := strconv.ParseInt(r.URL.Query().Get(key), 10, 32)
	if err != nil || page < 1 || page > 1000 {
		return 1
	}
	return int32(page)
}

func messageURL(id pgtype.UUID) string {
	return "/messages/" + id.String()
}
