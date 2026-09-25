package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/starfederation/datastar-go/datastar"
)

// usersView lists the accounts for the Users settings page.
func (s *Server) usersView(me *auth.User) views.UsersView {
	v := views.UsersView{Me: me.Username}
	for _, a := range s.accounts.List() {
		v.Accounts = append(v.Accounts, views.UserRow{
			Username: a.Username, Name: a.Name, Admin: a.Role == config.RoleAdmin,
			Created: a.CreatedAt.Format("2 Jan 2006"), Known: !a.CreatedAt.IsZero(),
		})
	}
	return v
}

// patchUsers answers a Users action with the refreshed page section.
func (s *Server) patchUsers(w http.ResponseWriter, r *http.Request, me *auth.User, st views.SourceStatus, signals map[string]any) {
	patchSource(w, r, views.UsersSection(s.usersView(me), st), signals)
}

// accountProblem says what went wrong with an account change, in words.
func accountProblem(err error) views.SourceStatus {
	switch {
	case errors.Is(err, auth.ErrAccountExists), errors.Is(err, auth.ErrInvalidAccount),
		errors.Is(err, auth.ErrInvalidRole), errors.Is(err, auth.ErrLastAdmin), errors.Is(err, auth.ErrNoAccount):
		msg := err.Error()
		return failed("%s.", strings.ToUpper(msg[:1])+msg[1:])
	}
	return failed(saveFailed)
}

// handleCreateUser adds an account from the "Add an account" form.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	me := s.adminAPI(w, r)
	if me == nil {
		return
	}
	var sig struct {
		Username string `json:"newUsername"`
		Name     string `json:"newName"`
		Password string `json:"newPassword"`
		Role     string `json:"newRole"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	if err := datastar.ReadSignals(r, &sig); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if err := s.accounts.Create(sig.Username, sig.Name, sig.Password, sig.Role); err != nil {
		s.patchUsers(w, r, me, accountProblem(err), map[string]any{"newPassword": ""})
		return
	}
	name := strings.ToLower(strings.TrimSpace(sig.Username))
	s.patchUsers(w, r, me, succeeded("%s can sign in now as %s.", accountName(s.accounts, name), name),
		map[string]any{"newUsername": "", "newName": "", "newPassword": "", "newRole": config.RoleViewer})
}

func accountName(accounts *auth.Accounts, username string) string {
	if u := accounts.Lookup(username); u != nil {
		return u.Name
	}
	return username
}

// handleUserAction resets a password, changes a role or deletes an account.
// Administrators cannot demote or delete themselves: another one has to, so
// nobody locks themselves out by accident.
func (s *Server) handleUserAction(w http.ResponseWriter, r *http.Request) {
	me := s.adminAPI(w, r)
	if me == nil {
		return
	}
	username := strings.ToLower(r.PathValue("username"))
	action := r.PathValue("action")
	if role := r.PathValue("role"); role != "" {
		if username == me.Username {
			s.patchUsers(w, r, me, failed("You cannot change the role of your own account; another administrator can."), nil)
			return
		}
		if err := s.accounts.SetRole(username, role); err != nil {
			s.patchUsers(w, r, me, accountProblem(err), nil)
			return
		}
		what := "a viewer"
		if role == config.RoleAdmin {
			what = "an administrator"
		}
		s.patchUsers(w, r, me, succeeded("%s is %s now.", accountName(s.accounts, username), what), nil)
		return
	}

	switch action {
	case "password":
		var sig struct {
			Password string `json:"resetPassword"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		if err := datastar.ReadSignals(r, &sig); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		if err := s.accounts.SetPassword(username, sig.Password); err != nil {
			s.patchUsers(w, r, me, accountProblem(err), map[string]any{"resetPassword": ""})
			return
		}
		msg := "New password saved; %s is signed out everywhere."
		if username == me.Username {
			msg = "New password saved for %s; your other browsers are signed out."
			if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
				s.sessions.EndOtherSessionsOf(username, cookie.Value)
			}
		} else {
			s.sessions.EndSessionsOf(username)
		}
		s.patchUsers(w, r, me, succeeded(msg, accountName(s.accounts, username)),
			map[string]any{"resetPassword": "", "resetOpen": false})
	case "delete":
		if username == me.Username {
			s.patchUsers(w, r, me, failed("You cannot delete your own account; another administrator can."), nil)
			return
		}
		name := accountName(s.accounts, username)
		if err := s.accounts.Delete(username); err != nil {
			s.patchUsers(w, r, me, accountProblem(err), nil)
			return
		}
		s.sessions.EndSessionsOf(username)
		s.patchUsers(w, r, me, succeeded("%s's account is deleted.", name), nil)
	default:
		http.NotFound(w, r)
	}
}
