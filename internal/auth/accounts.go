package auth

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/moviestracker-app/internal/config"
)

// minPasswordLength keeps LAN-reachable accounts out of trivial guessing range.
const minPasswordLength = 10

var (
	// ErrSetupDone refuses a second "first admin": setup runs once.
	ErrSetupDone = errors.New("setup is already complete")
	// ErrInvalidAccount rejects an unusable username or password.
	ErrInvalidAccount = fmt.Errorf("username must be letters, digits, '.', '-' or '_'; password at least %d characters", minPasswordLength)
	// ErrAccountExists refuses a second account with the same username.
	ErrAccountExists = errors.New("an account with this username already exists")
	// ErrInvalidRole rejects a role other than administrator or viewer.
	ErrInvalidRole = errors.New("role must be administrator or viewer")
	// ErrNoAccount is an operation on an account that does not exist.
	ErrNoAccount = errors.New("no such account")
	// ErrLastAdmin keeps at least one administrator, who can manage the rest.
	ErrLastAdmin = errors.New("the last administrator cannot be removed or made a viewer")
)

// dummyHash is compared against when a username is unknown, so a failed
// sign-in takes as long whether or not the account exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("moviestracker-dummy-password"), bcrypt.DefaultCost)

// Accounts are the local user accounts, kept in the config store.
type Accounts struct {
	store *config.Store
	now   func() time.Time
}

// NewAccounts returns the accounts kept in store.
func NewAccounts(store *config.Store) *Accounts {
	return &Accounts{store: store, now: time.Now}
}

// NeedsSetup reports whether no account exists yet.
func (a *Accounts) NeedsSetup() bool {
	return len(a.store.State().Users) == 0
}

// CreateFirstAdmin creates the administrator of a fresh install. Usernames
// are case-insensitive and stored lowercase.
func (a *Accounts) CreateFirstAdmin(username, name, password string) (*User, error) {
	username = normalizeUsername(username)
	if !validUsername(username) || len(password) < minPasswordLength {
		return nil, ErrInvalidAccount
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = username
	}
	account := config.User{Username: username, Name: name, Role: config.RoleAdmin, PasswordHash: string(hash), CreatedAt: a.now()}
	err = a.store.Update(func(st *config.State) error {
		if len(st.Users) > 0 {
			return ErrSetupDone
		}
		st.Users = append(st.Users, account)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return userFrom(account), nil
}

// Account is a local account as the Users settings list it.
type Account struct {
	Username  string
	Name      string
	Role      string
	CreatedAt time.Time
}

// List returns every account, ordered by username.
func (a *Accounts) List() []Account {
	users := a.store.State().Users
	out := make([]Account, 0, len(users))
	for _, u := range users {
		out = append(out, Account{Username: u.Username, Name: u.Name, Role: u.Role, CreatedAt: u.CreatedAt})
	}
	slices.SortFunc(out, func(x, y Account) int { return strings.Compare(x.Username, y.Username) })
	return out
}

// Create adds an account with role RoleAdmin or RoleViewer.
func (a *Accounts) Create(username, name, password, role string) error {
	username = normalizeUsername(username)
	if !validUsername(username) || len(password) < minPasswordLength {
		return ErrInvalidAccount
	}
	if role != config.RoleAdmin && role != config.RoleViewer {
		return ErrInvalidRole
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = username
	}
	return a.store.Update(func(st *config.State) error {
		if slices.ContainsFunc(st.Users, func(u config.User) bool { return u.Username == username }) {
			return ErrAccountExists
		}
		st.Users = append(st.Users, config.User{Username: username, Name: name, Role: role, PasswordHash: string(hash), CreatedAt: a.now()})
		return nil
	})
}

// SetPassword replaces an account's password.
func (a *Accounts) SetPassword(username, password string) error {
	if len(password) < minPasswordLength {
		return ErrInvalidAccount
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return a.change(username, func(st *config.State, i int) error {
		st.Users[i].PasswordHash = string(hash)
		return nil
	})
}

// SetRole makes an account an administrator or a viewer.
func (a *Accounts) SetRole(username, role string) error {
	if role != config.RoleAdmin && role != config.RoleViewer {
		return ErrInvalidRole
	}
	return a.change(username, func(st *config.State, i int) error {
		if role != config.RoleAdmin && lastAdmin(st, i) {
			return ErrLastAdmin
		}
		st.Users[i].Role = role
		return nil
	})
}

// Delete removes an account.
func (a *Accounts) Delete(username string) error {
	return a.change(username, func(st *config.State, i int) error {
		if lastAdmin(st, i) {
			return ErrLastAdmin
		}
		st.Users = slices.Delete(st.Users, i, i+1)
		return nil
	})
}

// change applies fn to the account named username, saved atomically.
func (a *Accounts) change(username string, fn func(st *config.State, i int) error) error {
	username = normalizeUsername(username)
	return a.store.Update(func(st *config.State) error {
		i := slices.IndexFunc(st.Users, func(u config.User) bool { return u.Username == username })
		if i < 0 {
			return ErrNoAccount
		}
		return fn(st, i)
	})
}

// lastAdmin reports whether account i is the only administrator.
func lastAdmin(st *config.State, i int) bool {
	if st.Users[i].Role != config.RoleAdmin {
		return false
	}
	admins := 0
	for _, u := range st.Users {
		if u.Role == config.RoleAdmin {
			admins++
		}
	}
	return admins == 1
}

// Authenticate returns the account whose username and password match.
func (a *Accounts) Authenticate(username, password string) (*User, error) {
	account, ok := a.find(normalizeUsername(username))
	hash := dummyHash
	if ok {
		hash = []byte(account.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || !ok {
		return nil, ErrInvalidCredentials
	}
	return userFrom(account), nil
}

// Lookup returns the current account for username, or nil once it is gone.
func (a *Accounts) Lookup(username string) *User {
	if account, ok := a.find(username); ok {
		return userFrom(account)
	}
	return nil
}

func (a *Accounts) find(username string) (config.User, bool) {
	for _, u := range a.store.State().Users {
		if u.Username == username {
			return u, true
		}
	}
	return config.User{}, false
}

func userFrom(u config.User) *User {
	return &User{Username: u.Username, Name: u.Name, Role: u.Role}
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func validUsername(username string) bool {
	if username == "" || len(username) > 64 {
		return false
	}
	for _, r := range username {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(".-_", r) {
			return false
		}
	}
	return true
}
