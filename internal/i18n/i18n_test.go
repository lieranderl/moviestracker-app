package i18n_test

import (
	"context"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
)

func TestTextIsEnglishUnlessTheContextAsksForRussian(t *testing.T) {
	ctx := context.Background()
	if got := i18n.T(ctx, "Sign in"); got != "Sign in" {
		t.Errorf("English T = %q", got)
	}
	ru := i18n.WithLang(ctx, i18n.Russian)
	if got := i18n.T(ru, "Sign in"); got != "Войти" {
		t.Errorf("Russian T = %q, want Войти", got)
	}
	if got := i18n.T(ru, "a sentence nobody translated"); got != "a sentence nobody translated" {
		t.Errorf("untranslated text = %q, want the English", got)
	}
	if got := i18n.Tf(ru, "Sign in as %s", "anna"); got != "Войти как anna" {
		t.Errorf("Tf = %q", got)
	}
}

func TestCountsTakeTheirLanguagesPluralForm(t *testing.T) {
	en := context.Background()
	ru := i18n.WithLang(en, i18n.Russian)
	cases := []struct {
		ctx  context.Context
		n    int
		want string
	}{
		{en, 1, "1 season"},
		{en, 2, "2 seasons"},
		{ru, 1, "1 сезон"},
		{ru, 3, "3 сезона"},
		{ru, 5, "5 сезонов"},
		{ru, 11, "11 сезонов"},
		{ru, 21, "21 сезон"},
		{ru, 22, "22 сезона"},
		{ru, 112, "112 сезонов"},
	}
	for _, c := range cases {
		if got := i18n.N(c.ctx, c.n, "%d season", "%d seasons"); got != c.want {
			t.Errorf("N(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestBrowsersPreferredLanguageIsChosenWhenSupported(t *testing.T) {
	cases := map[string]i18n.Lang{
		"":                              i18n.English,
		"ru-RU,ru;q=0.9,en-US;q=0.8":    i18n.Russian,
		"en-US,en;q=0.9,ru;q=0.8":       i18n.English,
		"de-DE,de;q=0.9,ru;q=0.5":       i18n.Russian,
		"fr, en;q=0.2, ru;q=0.7":        i18n.Russian,
		"ja":                            i18n.English,
		"ru;q=0":                        i18n.English,
		"RU":                            i18n.Russian,
		"garbage;;;,,q=bogus":           i18n.English,
		"uk-UA,uk;q=0.9,ru;q=0.8,en;q=": i18n.Russian,
	}
	for header, want := range cases {
		if got := i18n.Negotiate(header); got != want {
			t.Errorf("Negotiate(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestOnlySupportedLanguagesParse(t *testing.T) {
	for in, want := range map[string]i18n.Lang{"en": i18n.English, "ru": i18n.Russian} {
		if got, ok := i18n.Parse(in); !ok || got != want {
			t.Errorf("Parse(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "de", "ru-RU", "EN", "<script>"} {
		if _, ok := i18n.Parse(in); ok {
			t.Errorf("Parse(%q) accepted", in)
		}
	}
	if i18n.Russian.TMDB() != "ru-RU" || i18n.English.TMDB() != "en-US" {
		t.Errorf("TMDB tags = %q, %q", i18n.Russian.TMDB(), i18n.English.TMDB())
	}
	if i18n.Russian.Name() != "Русский" || i18n.English.Name() != "English" {
		t.Errorf("names = %q, %q", i18n.Russian.Name(), i18n.English.Name())
	}
}

func TestDatesNameTheMonthInTheLanguage(t *testing.T) {
	day := time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC)
	if got := i18n.Date(context.Background(), day); got != "7 Mar 2026" {
		t.Errorf("English date = %q", got)
	}
	if got := i18n.Date(i18n.WithLang(context.Background(), i18n.Russian), day); got != "7 мар. 2026" {
		t.Errorf("Russian date = %q", got)
	}
}
