package torrserver_test

import (
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

func FuzzParsedServerAddressKeepsNoLogin(f *testing.F) {
	for _, raw := range []string{"http://localhost:8090", " https://host/ ", "http://user:secret@host:8090/", "http://user@host", "ftp://host", "host:8090", "http://", "http://[::1]:8090"} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		addr, _, _, err := torrserver.ParseServerURL(raw)
		if err != nil {
			return
		}
		if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
			t.Fatalf("ParseServerURL(%q) address = %q", raw, addr)
		}
		again, user, password, err := torrserver.ParseServerURL(addr)
		if err != nil {
			t.Fatalf("ParseServerURL(%q) address %q does not parse again: %v", raw, addr, err)
		}
		if user != "" || password != "" {
			t.Errorf("ParseServerURL(%q) address %q still has a login", raw, addr)
		}
		if again != addr {
			t.Errorf("ParseServerURL(%q) address %q parses to %q", raw, addr, again)
		}
	})
}
