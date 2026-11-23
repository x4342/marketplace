package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/x4342/marketplace/internal/administration"
	"github.com/x4342/marketplace/internal/authz"
	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/web/templates"
)

type platformPages struct {
	identityPages
	queries *db.Queries
	service administration.Service
}

func (p platformPages) mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin", p.dashboard)
	mux.HandleFunc("GET /fragments/status", p.status)
	mux.HandleFunc("GET /admin/status", p.status)
	mux.HandleFunc("GET /admin/users", p.users)
	mux.HandleFunc("POST /admin/users/role", p.role)
	mux.HandleFunc("GET /admin/categories", p.categories)
	mux.HandleFunc("GET /admin/categories/{id}", p.categories)
	mux.HandleFunc("POST /admin/categories", p.saveCategory)
	mux.HandleFunc("POST /admin/categories/{id}", p.saveCategory)
	mux.HandleFunc("GET /staff", p.staff)
}

func (p platformPages) requireCapability(w http.ResponseWriter, r *http.Request, capability authz.Capability) (db.ActiveSessionRow, bool) {
	actor, ok := p.requireSession(w, r)
	if !ok {
		return actor, false
	}
	if !authz.Allows(actor.Role, capability) {
		p.render(w, r, http.StatusForbidden, templates.Problem("Access denied", "Your account does not have permission to use this page."))
		return actor, false
	}
	return actor, true
}

func (p platformPages) dashboard(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.requireCapability(w, r, authz.Platform)
	if !ok {
		return
	}
	status := p.app.status(r.Context())
	var data templates.PlatformDashboardData
	data.Status = status
	var err error
	if status.DatabaseReady {
		data.Roles, err = p.queries.AdminRoleAudit(r.Context(), actor.ID)
		if err == nil {
			data.Categories, err = p.queries.AdminCategoryAudit(r.Context(), actor.ID)
		}
	}
	data.AuditUnavailable = err != nil || !status.DatabaseReady
	p.render(w, r, http.StatusOK, templates.PlatformDashboard(data))
}

func (p platformPages) status(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.requireCapability(w, r, authz.Platform); ok {
		p.render(w, r, http.StatusOK, templates.SystemStatus(p.app.status(r.Context())))
	}
}

func (p platformPages) staff(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.requireCapability(w, r, authz.Staff); ok {
		p.render(w, r, http.StatusOK, templates.StaffWorkspace())
	}
}

func (p platformPages) users(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.requireCapability(w, r, authz.Platform)
	if !ok {
		return
	}
	p.renderUsers(w, r, actor, http.StatusOK, "")
}

func (p platformPages) renderUsers(w http.ResponseWriter, r *http.Request, actor db.ActiveSessionRow, status int, problem string) {
	prefix := identity.NormalizeUsername(r.URL.Query().Get("q"))
	if len(prefix) > 30 {
		prefix = ""
	}
	page := inboxPage(r)
	rows, err := p.queries.AdminUsers(r.Context(), db.AdminUsersParams{Actor: actor.ID, Prefix: prefix, PageOffset: 30 * (page - 1)})
	if err != nil {
		p.failure(w, r, err)
		return
	}
	more := len(rows) > 30
	if more {
		rows = rows[:30]
	}
	p.render(w, r, status, templates.PlatformUsers(rows, actor.Username, csrfFor(w, r), prefix, page, more, r.URL.Query().Get("saved") == "1", problem))
}

func (p platformPages) role(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.requireCapability(w, r, authz.Platform)
	if !ok {
		return
	}
	if !validForm(w, r) || r.PostForm.Get("confirm") != "1" {
		p.denied(w, r)
		return
	}
	if !p.requireFreshStaff(w, r, actor) {
		return
	}
	err := p.accounts.ChangeRole(r.Context(), actor.ID, r.PostForm.Get("username"), r.PostForm.Get("previous"), r.PostForm.Get("role"))
	switch {
	case err == nil:
		http.Redirect(w, r, "/admin/users?saved=1", http.StatusSeeOther)
	case errors.Is(err, identity.ErrRoleChanged), errors.Is(err, identity.ErrLastAdministrator):
		p.renderUsers(w, r, actor, http.StatusConflict, err.Error())
	case errors.Is(err, identity.ErrOperator):
		p.denied(w, r)
	default:
		p.failure(w, r, err)
	}
}

func (p platformPages) categories(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.requireCapability(w, r, authz.Platform)
	if !ok {
		return
	}
	p.renderCategories(w, r, actor.ID, http.StatusOK, administration.Category{Active: true}, "")
}

func (p platformPages) saveCategory(w http.ResponseWriter, r *http.Request) {
	actor, ok := p.requireCapability(w, r, authz.Platform)
	if !ok {
		return
	}
	if !validForm(w, r) || r.PostForm.Get("confirm") != "1" {
		p.denied(w, r)
		return
	}
	c := administration.Category{
		Name: r.PostForm.Get("name"), Description: r.PostForm.Get("description"),
		Slug: strings.TrimSpace(r.PostForm.Get("slug")), Active: r.PostForm.Get("active") == "1",
	}
	if value := r.PathValue("id"); value != "" {
		if err := c.ID.Scan(value); err != nil || !c.ID.Valid {
			p.failure(w, r, pgx.ErrNoRows)
			return
		}
	}
	c.Revision, _ = strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
	if value := r.PostForm.Get("parent"); value != "" {
		if err := c.Parent.Scan(value); err != nil || !c.Parent.Valid {
			p.renderCategories(w, r, actor.ID, http.StatusUnprocessableEntity, c, administration.ErrParent.Error())
			return
		}
	}
	order, err := strconv.ParseInt(r.PostForm.Get("display_order"), 10, 32)
	if err != nil {
		p.renderCategories(w, r, actor.ID, http.StatusUnprocessableEntity, c, administration.ErrOrder.Error())
		return
	}
	c.DisplayOrder = int32(order)
	switch err := p.service.SaveCategory(r.Context(), actor.ID, c); {
	case err == nil:
		http.Redirect(w, r, "/admin/categories?saved=1", http.StatusSeeOther)
	case errors.Is(err, administration.ErrChanged), errors.Is(err, administration.ErrTaken):
		p.renderCategories(w, r, actor.ID, http.StatusConflict, c, err.Error())
	case errors.Is(err, administration.ErrOrder), errors.Is(err, administration.ErrName), errors.Is(err, administration.ErrSlug), errors.Is(err, administration.ErrParent):
		p.renderCategories(w, r, actor.ID, http.StatusUnprocessableEntity, c, err.Error())
	default:
		p.failure(w, r, err)
	}
}

func (p platformPages) renderCategories(w http.ResponseWriter, r *http.Request, actor pgtype.UUID, status int, form administration.Category, problem string) {
	rows, err := p.queries.AdminCategories(r.Context(), actor)
	if err != nil {
		p.failure(w, r, err)
		return
	}
	if value := r.PathValue("id"); value != "" && status == http.StatusOK {
		var id pgtype.UUID
		if err := id.Scan(value); err != nil {
			p.failure(w, r, pgx.ErrNoRows)
			return
		}
		row, err := p.queries.AdminCategory(r.Context(), db.AdminCategoryParams{ID: id, Actor: actor})
		if err != nil {
			p.failure(w, r, err)
			return
		}
		form = administration.Category{ID: row.ID, Revision: row.Revision, Name: row.Name, Slug: row.Slug, Description: row.Description, Active: row.Active, DisplayOrder: row.DisplayOrder}
		for _, item := range rows {
			if row.ParentSlug.Valid && row.ParentSlug.String == item.Slug {
				form.Parent = item.ID
				break
			}
		}
	}
	p.render(w, r, status, templates.PlatformCategories(rows, form, csrfFor(w, r), r.URL.Query().Get("saved") == "1", problem))
}

func (p platformPages) failure(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		p.render(w, r, http.StatusNotFound, templates.Problem("Record unavailable", "Open the current records from the administrator dashboard."))
	case errors.Is(err, administration.ErrForbidden):
		p.denied(w, r)
	default:
		p.log.Error("platform administrator operation failed")
		p.render(w, r, http.StatusServiceUnavailable, templates.Problem("Administration unavailable", "Try again shortly."))
	}
}
