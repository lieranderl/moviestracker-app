package i18n_test

import (
	"slices"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
)

func FuzzNegotiateAlwaysPicksASupportedLanguage(f *testing.F) {
	for _, header := range []string{"", "ru", "ru-RU,ru;q=0.9,en;q=0.8", "en;q=0.1,ru;q=0.2", "ru;q=NaN", "ru;q=", "de,fr;q=0.5", ",;,"} {
		f.Add(header)
	}
	f.Fuzz(func(t *testing.T, header string) {
		if got := i18n.Negotiate(header); !slices.Contains(i18n.Supported, got) {
			t.Errorf("Negotiate(%q) = %q", header, got)
		}
	})
}
