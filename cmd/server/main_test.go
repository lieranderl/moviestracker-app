package main

import (
	"crypto/rand"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/engine"
)

func TestEnvBool(t *testing.T) {
	t.Run("missing value uses secure default", func(t *testing.T) {
		if !envBool("DATASTAR_TEST_MISSING_BOOL", true) {
			t.Fatal("missing value did not use true default")
		}
	})

	t.Run("explicit false", func(t *testing.T) {
		t.Setenv("DATASTAR_TEST_BOOL", "false")
		if envBool("DATASTAR_TEST_BOOL", true) {
			t.Fatal("explicit false parsed as true")
		}
	})

	t.Run("invalid value uses secure default", func(t *testing.T) {
		t.Setenv("DATASTAR_TEST_BOOL", "definitely-not-a-bool")
		if !envBool("DATASTAR_TEST_BOOL", true) {
			t.Fatal("invalid value did not fail closed to true")
		}
	})
}

func TestEnvList(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty"},
		{name: "whitespace", value: "   "},
		{name: "two values", value: "10.0.0.0/8, 192.0.2.0/24", want: []string{"10.0.0.0/8", "192.0.2.0/24"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATASTAR_TEST_LIST", tt.value)
			if got := envList("DATASTAR_TEST_LIST"); !slices.Equal(got, tt.want) {
				t.Fatalf("envList() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnInstallThatAlreadyHasAccountsKeepsItsTorrServer(t *testing.T) {
	sup := engine.New(engine.Config{Binary: "/bin/sh", Dir: t.TempDir()}) // a TorrServer program is available
	for _, tc := range []struct {
		name     string
		accounts []config.User
		want     string
	}{
		{"fresh install", nil, config.EngineManaged},
		{"install from before engine modes", []config.User{{Username: "test"}}, config.EngineExternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := config.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Update(func(st *config.State) error { st.Users = tc.accounts; return nil }); err != nil {
				t.Fatal(err)
			}
			if err := settleEngineMode(store, sup); err != nil {
				t.Fatal(err)
			}
			if got := store.State().TorrServer.Mode; got != tc.want {
				t.Errorf("mode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTheServerNoticesWhenTheAppThatStartedItIsGone(t *testing.T) {
	select {
	case <-parentGone(os.Getppid(), 10*time.Millisecond):
		t.Fatal("parent reported gone while it is alive")
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case <-parentGone(os.Getppid()+1_000_000, 10*time.Millisecond):
	case <-time.After(time.Second):
		t.Fatal("a launcher that is no longer the parent was not noticed")
	}
}

func TestTheSetupCodeIsEightUnambiguousCharactersFromEntropy(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		code, err := newSetupCode(rand.Reader)
		if err != nil {
			t.Fatalf("newSetupCode(): %v", err)
		}
		if !regexp.MustCompile(`^[23456789ABCDEFGHJKMNPQRSTUVWXYZ]{4}-[23456789ABCDEFGHJKMNPQRSTUVWXYZ]{4}$`).MatchString(code) {
			t.Fatalf("setup code %q is not XXXX-XXXX without 0/O, 1/I/L", code)
		}
		seen[code] = true
	}
	if len(seen) < 20 {
		t.Errorf("20 setup codes had only %d distinct values", len(seen))
	}
	if _, err := newSetupCode(strings.NewReader("short")); err == nil {
		t.Error("a failing entropy source still gave a setup code")
	}
}
