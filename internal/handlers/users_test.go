package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/config"
)

// signedOut reports whether cookie no longer opens a page.
func signedOut(t *testing.T, l *local, cookie *http.Cookie) bool {
	t.Helper()
	rr := l.do(t, httptest.NewRequest(http.MethodGet, "/dashboard", nil), cookie)
	return rr.Code == http.StatusSeeOther && rr.Header().Get("Location") == "/login"
}

func TestOnlyAdministratorsSeeTheUsersPage(t *testing.T) {
	l := newLocal(t, func(st *config.Store) {
		addAccount(t, st, "admin", config.RoleAdmin)
		addAccount(t, st, "anna", config.RoleViewer)
	})
	rr := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/users", nil), l.admin(t))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "anna") || !strings.Contains(rr.Body.String(), "Add an account") {
		t.Fatalf("admin GET /settings/users = %d, want the account list and the add form:\n%s", rr.Code, rr.Body)
	}
	viewer := l.signIn(t, l.accounts.Lookup("anna"))
	if rr := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/users", nil), viewer); rr.Code != http.StatusForbidden {
		t.Errorf("viewer GET /settings/users = %d, want 403", rr.Code)
	}
	if rr := l.action(t, "/api/settings/users", `{"newUsername":"eve","newPassword":"eve password 1","newRole":"admin"}`, viewer); rr.Code != http.StatusForbidden {
		t.Errorf("viewer adding an account = %d, want 403", rr.Code)
	}
}

func TestAnAdministratorAddsAViewerWhoCanSignInButNotChangeSettings(t *testing.T) {
	l := newLocal(t, withAdmin(t))
	rr := l.action(t, "/api/settings/users", `{"newUsername":"Anna","newName":"Anna K","newPassword":"anna password","newRole":"viewer"}`, l.admin(t))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Anna K") || !strings.Contains(rr.Body.String(), "can sign in") {
		t.Fatalf("add account = %d:\n%s", rr.Code, rr.Body)
	}
	anna, err := l.accounts.Authenticate("anna", "anna password")
	if err != nil {
		t.Fatalf("the new account cannot sign in: %v", err)
	}
	if rr := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources", nil), l.signIn(t, anna)); rr.Code != http.StatusForbidden {
		t.Errorf("viewer GET /settings/sources = %d, want 403", rr.Code)
	}

	rr = l.action(t, "/api/settings/users", `{"newUsername":"anna","newPassword":"other password","newRole":"viewer"}`, l.admin(t))
	if !strings.Contains(rr.Body.String(), "already exists") {
		t.Errorf("a second anna was not refused:\n%s", rr.Body)
	}
}

func TestAResetPasswordSignsThatPersonOutEverywhere(t *testing.T) {
	l := newLocal(t, func(st *config.Store) {
		addAccount(t, st, "admin", config.RoleAdmin)
		addAccount(t, st, "anna", config.RoleViewer)
	})
	annasPhone := l.signIn(t, l.accounts.Lookup("anna"))
	rr := l.action(t, "/api/settings/users/anna/password", `{"resetPassword":"a brand new password"}`, l.admin(t))
	if !strings.Contains(rr.Body.String(), "New password saved") {
		t.Fatalf("reset password:\n%s", rr.Body)
	}
	if !signedOut(t, l, annasPhone) {
		t.Error("anna's phone is still signed in with the old password")
	}
	if _, err := l.accounts.Authenticate("anna", "a brand new password"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
}

// A new password is often a reaction to a leaked one: whoever used it
// must not stay signed in, but the browser making the change does.
func TestANewPasswordForYourselfSignsOutYourOtherBrowsers(t *testing.T) {
	l := newLocal(t, withAdmin(t))
	laptop, phone := l.admin(t), l.admin(t)
	rr := l.action(t, "/api/settings/users/admin/password", `{"resetPassword":"a brand new password"}`, laptop)
	if !strings.Contains(rr.Body.String(), "other browsers are signed out") {
		t.Fatalf("own password change does not say the other browsers are signed out:\n%s", rr.Body)
	}
	if !signedOut(t, l, phone) {
		t.Error("the phone is still signed in with the old password")
	}
	if signedOut(t, l, laptop) {
		t.Error("the browser that changed the password was signed out")
	}
}

func TestARoleChangeAppliesAtOnce(t *testing.T) {
	l := newLocal(t, func(st *config.Store) {
		addAccount(t, st, "admin", config.RoleAdmin)
		addAccount(t, st, "anna", config.RoleViewer)
	})
	anna := l.signIn(t, l.accounts.Lookup("anna"))
	l.action(t, "/api/settings/users/anna/role/admin", `{}`, l.admin(t))
	if rr := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/users", nil), anna); rr.Code != http.StatusOK {
		t.Errorf("anna after becoming an administrator: GET /settings/users = %d, want 200", rr.Code)
	}
	l.action(t, "/api/settings/users/anna/role/viewer", `{}`, l.admin(t))
	if rr := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/users", nil), anna); rr.Code != http.StatusForbidden {
		t.Errorf("anna back to viewer: GET /settings/users = %d, want 403", rr.Code)
	}
}

func TestDeletingAnAccountSignsItOutButAdministratorsCannotDeleteThemselves(t *testing.T) {
	l := newLocal(t, func(st *config.Store) {
		addAccount(t, st, "admin", config.RoleAdmin)
		addAccount(t, st, "anna", config.RoleViewer)
	})
	anna := l.signIn(t, l.accounts.Lookup("anna"))
	admin := l.admin(t)
	l.action(t, "/api/settings/users/anna/delete", `{}`, admin)
	if l.accounts.Lookup("anna") != nil || !signedOut(t, l, anna) {
		t.Error("anna's account or session survived deletion")
	}

	rr := l.action(t, "/api/settings/users/admin/delete", `{}`, admin)
	if !strings.Contains(rr.Body.String(), "your own account") || l.accounts.Lookup("admin") == nil {
		t.Errorf("an administrator deleted their own account:\n%s", rr.Body)
	}
	rr = l.action(t, "/api/settings/users/admin/role/viewer", `{}`, admin)
	if !strings.Contains(rr.Body.String(), "your own account") || !l.accounts.Lookup("admin").IsAdmin() {
		t.Errorf("an administrator demoted themselves:\n%s", rr.Body)
	}
}
