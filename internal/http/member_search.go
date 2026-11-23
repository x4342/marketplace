package web

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/web/templates"
)

var memberPrefix = regexp.MustCompile(`^[a-z0-9_]{1,30}$`)

func (p catalogPages) memberSearch(w http.ResponseWriter, r *http.Request) {
	query := identity.NormalizeUsername(r.URL.Query().Get("q"))
	page := inboxPage(r)
	session, err := p.accounts.Active(r.Context(), cookieValue(r, sessionCookie))
	if err != nil && !errors.Is(err, identity.ErrInvalid) {
		p.catalogError(w, r, err)
		return
	}
	var csrf string
	if err == nil {
		csrf = csrfFor(w, r)
	}
	var rows []db.SearchMembersRow
	var problem string
	if query != "" {
		if memberPrefix.MatchString(query) {
			var err error
			rows, err = p.queries.SearchMembers(r.Context(), db.SearchMembersParams{Viewer: session.ID, Prefix: query, PageOffset: 24 * (page - 1)})
			if err != nil {
				p.catalogError(w, r, err)
				return
			}
		} else {
			problem = "Enter up to 30 ASCII letters or numbers. Existing usernames can include underscores."
			query = ""
		}
	}
	more := len(rows) > 24
	if more {
		rows = rows[:24]
	}
	r = r.WithContext(templates.WithHeaderSearch(r.Context(), query, "users"))
	p.render(w, r, http.StatusOK, templates.MemberSearch(query, problem, csrf, rows, page, more))
}
