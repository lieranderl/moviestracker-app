package views

import (
	"context"
	"strings"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
)

// tr is msg in the language of the page being rendered.
func tr(ctx context.Context, msg string) string { return i18n.T(ctx, msg) }

// trf formats args with the translation of format.
func trf(ctx context.Context, format string, args ...any) string {
	return i18n.Tf(ctx, format, args...)
}

// trn is a count in its plural form: one and other are the English forms.
func trn(ctx context.Context, n int, one, other string) string { return i18n.N(ctx, n, one, other) }

// pageLang is the page's language code, for <html lang>.
func pageLang(ctx context.Context) string { return string(i18n.FromContext(ctx)) }

// jsQuote escapes text for a single-quoted JavaScript string.
var jsQuote = strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`, "\u2028", `\u2028`, "\u2029", `\u2029`)

// jsString quotes s for a Datastar expression, in single quotes like the
// rest of the page's expressions.
func jsString(s string) string { return "'" + jsQuote.Replace(s) + "'" }

// trJS is msg translated and quoted for a Datastar expression.
func trJS(ctx context.Context, msg string) string { return jsString(tr(ctx, msg)) }

// runtime renders minutes as "2h 28m", "45m" or "" in the page's language.
func runtime(ctx context.Context, minutes int) string {
	if minutes <= 0 {
		return ""
	}
	h, m := minutes/60, minutes%60
	switch {
	case h == 0:
		return trf(ctx, "%dm", m)
	case m == 0:
		return trf(ctx, "%dh", h)
	default:
		return trf(ctx, "%dh %dm", h, m)
	}
}

// ratingTitle is a rating badge's tooltip: "IMDb: 8.4/10 (2.1M votes)".
func ratingTitle(ctx context.Context, source, rating, votes string) string {
	if rating == "" || rating == "0" {
		rating = "NR"
	}
	if votes != "" {
		return trf(ctx, "%s: %s/10 (%s votes)", source, rating, votes)
	}
	return source + ": " + rating + "/10"
}
