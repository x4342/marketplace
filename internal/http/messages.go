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
	"github.com/x4342/marketplace/web/templates"
)

var errThreadBlocked = errors.New("conversation blocked")

type messagePages struct {
	identityPages
	queries *db.Queries
	pool    *pgxpool.Pool
}

func (p messagePages) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /messages", p.inbox)
	mux.HandleFunc("GET /messages/{id}", p.thread)
	mux.HandleFunc("GET /messages/{id}/entries", p.fragment)
	mux.HandleFunc("POST /listings/{id}/conversation", p.open)
	mux.HandleFunc("POST /messages/{id}/send", p.send)
	mux.HandleFunc("POST /messages/{id}/read", p.markRead)
	mux.HandleFunc("POST /messages/{id}/block", p.block)
	mux.HandleFunc("POST /messages/{id}/report", p.report)
	mux.HandleFunc("GET /notifications", p.notifications)
	mux.HandleFunc("GET /notifications/{id}", p.notification)
}

func (p messagePages) inbox(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	page := inboxPage(r)
	rows, err := p.queries.Inbox(r.Context(), db.InboxParams{Actor: session.ID, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	unread, err := p.queries.UnreadConversations(r.Context(), session.ID)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, templates.Inbox(theme(r), rows, page, unread))
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

func (p messagePages) loadThread(w http.ResponseWriter, r *http.Request) (templates.ThreadData, bool) {
	session, ok := p.requireSession(w, r)
	if !ok {
		return templates.ThreadData{}, false
	}
	id := listingID(r)
	thread, err := p.queries.Thread(r.Context(), db.ThreadParams{ThreadID: id, Actor: session.ID})
	if err != nil {
		p.failure(w, r, err)
		return templates.ThreadData{}, false
	}
	before := int64(math.MaxInt64)
	if number, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil && number > 0 {
		before = number
	}
	rows, err := p.queries.ThreadMessages(r.Context(), db.ThreadMessagesParams{ThreadID: id, Actor: session.ID, BeforeSequence: before})
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
		data.Events, err = p.queries.ThreadEvents(r.Context(), db.ThreadEventsParams{ThreadID: id, Actor: session.ID})
		if err != nil {
			p.failure(w, r, err)
			return templates.ThreadData{}, false
		}
	}
	return data, true
}

func (p messagePages) thread(w http.ResponseWriter, r *http.Request) {
	if data, ok := p.loadThread(w, r); ok {
		p.render(w, r, http.StatusOK, templates.MessageThread(theme(r), data, r.URL.Query().Get("updated")))
	}
}

func (p messagePages) fragment(w http.ResponseWriter, r *http.Request) {
	if data, ok := p.loadThread(w, r); ok {
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
		p.problem(w, r, http.StatusUnprocessableEntity, "Message could not be sent", "Enter a message between 1 and 2,000 characters.")
		return
	}
	err := pgx.BeginFunc(r.Context(), p.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
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
		p.problem(w, r, http.StatusForbidden, "Conversation blocked", "New messages are paused in this conversation.")
		return
	}
	if err != nil {
		p.failure(w, r, err)
		return
	}
	http.Redirect(w, r, messageURL(id)+"?updated=sent", http.StatusSeeOther)
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
	p.render(w, r, http.StatusOK, templates.Notifications(theme(r), rows, orderNotices, reviewNotices, page))
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
	p.render(w, r, status, templates.Problem(theme(r), title, message))
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
