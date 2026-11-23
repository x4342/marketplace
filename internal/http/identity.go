package web

import (
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/identity"
	"github.com/x4342/marketplace/web/templates"
)

const (
	sessionCookie = "marketplace_session"
	csrfCookie    = "marketplace_csrf"
)

type identityPages struct {
	app
	accounts *identity.Service
}

func (a app) mountIdentity(mux *http.ServeMux, accounts *identity.Service) {
	pages := identityPages{app: a, accounts: accounts}
	for _, form := range []templates.AuthForm{
		{Path: "/register", Title: "Create account", Action: "Create account", NewPassword: true},
		{Path: "/login", Title: "Sign in", Action: "Sign in"},
		{Path: "/recover", Title: "Recover account", Action: "Reset password", NewPassword: true, Recovery: true},
	} {
		mux.HandleFunc("GET "+form.Path, func(w http.ResponseWriter, r *http.Request) {
			formData := form
			formData.CSRF = csrfFor(w, r)
			a.render(w, r, http.StatusOK, templates.Credentials(formData))
		})
		mux.HandleFunc("POST "+form.Path, func(w http.ResponseWriter, r *http.Request) {
			pages.submit(w, r, form)
		})
	}
	mux.HandleFunc("GET /account", pages.account)
	mux.HandleFunc("GET /logout", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
	})
	mux.HandleFunc("POST /logout", pages.logout)
	mux.HandleFunc("GET /admin/moderation", pages.rolePage("Moderation", func(s db.ActiveSessionRow) bool { return s.Moderator }))
	mux.HandleFunc("GET /admin/finance", pages.rolePage("Finance", func(s db.ActiveSessionRow) bool { return s.FinanceAdmin }))
}

func (p identityPages) submit(w http.ResponseWriter, r *http.Request, form templates.AuthForm) {
	if !validForm(w, r) {
		p.denied(w, r)
		return
	}
	form.Username = identity.NormalizeUsername(r.PostForm.Get("username"))
	form.CSRF = cookieValue(r, csrfCookie)
	password := r.PostForm.Get("password")
	var enrollment identity.Enrollment
	var err error
	switch form.Path {
	case "/register":
		enrollment, err = p.accounts.Register(r.Context(), form.Username, password)
	case "/login":
		enrollment.Session, err = p.accounts.Login(r.Context(), form.Username, password, cookieValue(r, sessionCookie))
	case "/recover":
		enrollment, err = p.accounts.Recover(r.Context(), form.Username, r.PostForm.Get("code"), password)
	}

	status := http.StatusUnprocessableEntity
	switch {
	case err == nil:
		setPrivateCookie(w, r, sessionCookie, enrollment.Session, 12*60*60)
		setPrivateCookie(w, r, csrfCookie, identity.NewToken(), 12*60*60)
		if len(enrollment.Codes) > 0 {
			p.render(w, r, http.StatusOK, templates.RecoveryCodes(enrollment.Codes))
			return
		}
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	case errors.Is(err, identity.ErrConflict):
		form.Error = "Choose a different username."
		status = http.StatusConflict
	case errors.Is(err, identity.ErrLimited):
		form.Error = "Too many attempts. Wait a minute before trying again."
		status = http.StatusTooManyRequests
		w.Header().Set("Retry-After", "60")
	case errors.Is(err, identity.ErrInvalid):
		form.Error = "Check the details you entered and try again."
	default:
		p.log.Error("identity request failed")
		form.Error = "This action is temporarily unavailable. Try again shortly."
		status = http.StatusServiceUnavailable
	}
	p.render(w, r, status, templates.Credentials(form))
}

func (p identityPages) account(w http.ResponseWriter, r *http.Request) {
	session, ok := p.requireSession(w, r)
	if ok {
		p.render(w, r, http.StatusOK, templates.Account(session.Username, csrfFor(w, r), session.Moderator, session.FinanceAdmin))
	}
}

func (p identityPages) logout(w http.ResponseWriter, r *http.Request) {
	if !validForm(w, r) {
		p.denied(w, r)
		return
	}
	session, ok := p.requireSession(w, r)
	if !ok {
		return
	}
	err := p.accounts.Logout(r.Context(), session.ID, cookieValue(r, sessionCookie))
	if err != nil && !errors.Is(err, identity.ErrInvalid) {
		p.unavailable(w, r)
		return
	}
	setPrivateCookie(w, r, sessionCookie, "", -1)
	setPrivateCookie(w, r, csrfCookie, "", -1)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (p identityPages) requireSession(w http.ResponseWriter, r *http.Request) (db.ActiveSessionRow, bool) {
	session, err := p.accounts.Active(r.Context(), cookieValue(r, sessionCookie))
	if errors.Is(err, identity.ErrInvalid) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	} else if err != nil {
		p.unavailable(w, r)
	}
	return session, err == nil
}

func (p identityPages) rolePage(title string, allowed func(db.ActiveSessionRow) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := p.requireSession(w, r)
		if !ok {
			return
		}
		if !allowed(session) {
			p.render(w, r, http.StatusForbidden, templates.Problem("Access denied", "Your account does not have permission to use this page."))
			return
		}
		p.render(w, r, http.StatusOK, templates.RoleAccess(title))
	}
}

func (p identityPages) denied(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, http.StatusForbidden, templates.Problem("Request denied", "Return to the account page and submit the form again."))
}

func (p identityPages) unavailable(w http.ResponseWriter, r *http.Request) {
	p.log.Error("session operation failed")
	p.render(w, r, http.StatusServiceUnavailable, templates.Problem("Account unavailable", "Try again shortly."))
}

func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func csrfFor(w http.ResponseWriter, r *http.Request) string {
	if csrf := cookieValue(r, csrfCookie); identity.ValidToken(csrf) {
		return csrf
	}
	csrf := identity.NewToken()
	setPrivateCookie(w, r, csrfCookie, csrf, 12*60*60)
	return csrf
}

func validForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		return false
	}
	return validCSRF(r)
}

func validCSRF(r *http.Request) bool {
	token := cookieValue(r, csrfCookie)
	return identity.ValidToken(token) && len(r.PostForm["csrf"]) == 1 &&
		subtle.ConstantTimeCompare([]byte(token), []byte(r.PostForm.Get("csrf"))) == 1
}

func setPrivateCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}
