package auth_test

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
)

// withAdmin is a set-up install: its administrator is "admin".
func withAdmin(t *testing.T) *auth.Accounts {
	t.Helper()
	accounts := auth.NewAccounts(openStore(t, t.TempDir()))
	if _, err := accounts.CreateFirstAdmin("admin", "Admin", "admin password 1"); err != nil {
		t.Fatalf("CreateFirstAdmin(): %v", err)
	}
	return accounts
}

func TestAnAdminCanAddAViewerWhoCanSignIn(t *testing.T) {
	accounts := withAdmin(t)
	if err := accounts.Create("Anna", " Anna K ", "anna password", config.RoleViewer); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	anna, err := accounts.Authenticate("anna", "anna password")
	if err != nil {
		t.Fatalf("the new viewer cannot sign in: %v", err)
	}
	if anna.Name != "Anna K" || anna.IsAdmin() {
		t.Errorf("anna = %+v, want name Anna K and no admin rights", anna)
	}

	list := accounts.List()
	if len(list) != 2 || list[0].Username != "admin" || list[1].Username != "anna" || list[1].Role != config.RoleViewer || list[1].CreatedAt.IsZero() {
		t.Errorf("List() = %+v, want admin then anna (viewer, with a creation time)", list)
	}
}

func TestAddingAnAccountRefusesDuplicatesAndUnusableDetails(t *testing.T) {
	accounts := withAdmin(t)
	for _, tc := range []struct {
		name, username, password, role string
		want                           error
	}{
		{"same username, other case", "ADMIN", "a long password", config.RoleViewer, auth.ErrAccountExists},
		{"short password", "bob", "short", config.RoleViewer, auth.ErrInvalidAccount},
		{"bad username", "bob smith", "a long password", config.RoleViewer, auth.ErrInvalidAccount},
		{"unknown role", "bob", "a long password", "owner", auth.ErrInvalidRole},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := accounts.Create(tc.username, "", tc.password, tc.role); !errors.Is(err, tc.want) {
				t.Errorf("Create() = %v, want %v", err, tc.want)
			}
		})
	}
	if len(accounts.List()) != 1 {
		t.Errorf("refused accounts were kept: %+v", accounts.List())
	}
}

func TestAResetPasswordReplacesTheOldOne(t *testing.T) {
	accounts := withAdmin(t)
	if err := accounts.Create("anna", "", "anna password", config.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := accounts.SetPassword("anna", "short"); !errors.Is(err, auth.ErrInvalidAccount) {
		t.Errorf("SetPassword(short) = %v, want ErrInvalidAccount", err)
	}
	if err := accounts.SetPassword("anna", "a brand new password"); err != nil {
		t.Fatalf("SetPassword(): %v", err)
	}
	if _, err := accounts.Authenticate("anna", "anna password"); err == nil {
		t.Error("the old password still works")
	}
	if _, err := accounts.Authenticate("anna", "a brand new password"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
	if err := accounts.SetPassword("nobody", "a brand new password"); !errors.Is(err, auth.ErrNoAccount) {
		t.Errorf("SetPassword(nobody) = %v, want ErrNoAccount", err)
	}
}

func TestTheLastAdministratorCannotBeRemovedOrDemoted(t *testing.T) {
	accounts := withAdmin(t)
	if err := accounts.SetRole("admin", config.RoleViewer); !errors.Is(err, auth.ErrLastAdmin) {
		t.Errorf("demoting the only admin = %v, want ErrLastAdmin", err)
	}
	if err := accounts.Delete("admin"); !errors.Is(err, auth.ErrLastAdmin) {
		t.Errorf("deleting the only admin = %v, want ErrLastAdmin", err)
	}

	// With a second administrator, the first can step down and go.
	if err := accounts.Create("bob", "", "bob password1", config.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := accounts.SetRole("admin", config.RoleViewer); err != nil {
		t.Fatalf("SetRole(): %v", err)
	}
	if u := accounts.Lookup("admin"); u == nil || u.IsAdmin() {
		t.Errorf("admin after demotion = %+v, want a viewer", u)
	}
	if err := accounts.Delete("admin"); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if accounts.Lookup("admin") != nil {
		t.Error("a deleted account is still found")
	}
	if _, err := accounts.Authenticate("admin", "admin password 1"); err == nil {
		t.Error("a deleted account can still sign in")
	}
}

func TestEndingAnAccountsSessionsSignsOutEveryBrowserOfIt(t *testing.T) {
	sessions := auth.NewSessionManager(10, rand.Reader)
	phone, _ := sessions.CreateSession(&auth.User{Username: "anna"})
	laptop, _ := sessions.CreateSession(&auth.User{Username: "anna"})
	other, _ := sessions.CreateSession(&auth.User{Username: "bob"})

	sessions.EndSessionsOf("anna")

	if sessions.GetUser(phone) != nil || sessions.GetUser(laptop) != nil {
		t.Error("anna is still signed in somewhere")
	}
	if sessions.GetUser(other) == nil {
		t.Error("bob was signed out too")
	}
}
