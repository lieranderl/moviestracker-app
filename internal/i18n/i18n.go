// Package i18n translates the interface. English text is the key: a
// template writes T(ctx, "Sign in") and gets the text in the language the
// request carries, or the English when that language lacks a translation.
package i18n

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Lang is an interface language, by its ISO 639-1 code.
type Lang string

const (
	English Lang = "en"
	Russian Lang = "ru"
)

// Supported are the interface languages, in the order menus list them.
var Supported = []Lang{English, Russian}

// Parse returns the supported language with code s.
func Parse(s string) (Lang, bool) {
	for _, l := range Supported {
		if string(l) == s {
			return l, true
		}
	}
	return English, false
}

// Name is the language's name in itself, as a language menu shows it.
func (l Lang) Name() string {
	if l == Russian {
		return "Русский"
	}
	return "English"
}

// TMDB is the language tag TMDB's API takes.
func (l Lang) TMDB() string {
	if l == Russian {
		return "ru-RU"
	}
	return "en-US"
}

type langKey struct{}

// WithLang returns ctx carrying l.
func WithLang(ctx context.Context, l Lang) context.Context {
	return context.WithValue(ctx, langKey{}, l)
}

// FromContext is the language ctx carries: English when none.
func FromContext(ctx context.Context) Lang {
	if l, ok := ctx.Value(langKey{}).(Lang); ok {
		return l
	}
	return English
}

// T is msg in ctx's language.
func T(ctx context.Context, msg string) string {
	if FromContext(ctx) == Russian {
		if s, ok := ru[msg]; ok {
			return s
		}
	}
	return msg
}

// Tf formats args with the translation of format.
func Tf(ctx context.Context, format string, args ...any) string {
	return fmt.Sprintf(T(ctx, format), args...)
}

// N formats n with the plural form its count takes in ctx's language: one
// and other are the English forms ("%d season", "%d seasons").
func N(ctx context.Context, n int, one, other string) string {
	if FromContext(ctx) == Russian {
		if forms, ok := ruPlural[one]; ok {
			return fmt.Sprintf(forms[russianForm(n)], n)
		}
	}
	if n == 1 {
		return fmt.Sprintf(one, n)
	}
	return fmt.Sprintf(other, n)
}

// russianForm picks one (1, 21), few (2–4, 22) or many (0, 5–20, 25).
func russianForm(n int) int {
	n %= 100
	if n < 0 {
		n = -n
	}
	switch {
	case n%10 == 1 && n != 11:
		return 0
	case n%10 >= 2 && n%10 <= 4 && (n < 12 || n > 14):
		return 1
	default:
		return 2
	}
}

// Negotiate picks the supported language an Accept-Language header prefers
// most; English when it names none.
func Negotiate(header string) Lang {
	best, bestQ := English, 0.0
	for part := range strings.SplitSeq(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				continue
			}
			q = parsed
		}
		primary, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		l, ok := Parse(primary)
		if ok && q > bestQ {
			best, bestQ = l, q
		}
	}
	return best
}

// Has reports whether l has a translation of msg, a text or the one form
// of a count; English has every text.
func Has(l Lang, msg string) bool {
	if l == English {
		return true
	}
	_, text := ru[msg]
	_, count := ruPlural[msg]
	return text || count
}

// ruMonths are the months' short Russian names, in the genitive a date
// takes ("7 мар. 2026").
var ruMonths = [12]string{"янв.", "февр.", "мар.", "апр.", "мая", "июн.", "июл.", "авг.", "сент.", "окт.", "нояб.", "дек."}

// Date is t's day as "7 Mar 2026", in ctx's language.
func Date(ctx context.Context, t time.Time) string {
	if FromContext(ctx) == Russian {
		return fmt.Sprintf("%d %s %d", t.Day(), ruMonths[t.Month()-1], t.Year())
	}
	return t.Format("2 Jan 2006")
}
