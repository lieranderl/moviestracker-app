package views

import (
	"context"
	"slices"

	"github.com/a-h/templ"
)

// Site is which app a page belongs to: the local app (the zero value) or
// the cloud web app, whose visitors sign in with Google and use their own
// TorrServer from the browser.
type Site struct {
	Cloud bool
	// TorrServerCheck are the Datastar attributes, from the site's adapter,
	// by which the navbar's TorrServer status learns whether it answers:
	// they set $_tsNav to "online", "offline" or "none". Nil shows none.
	TorrServerCheck templ.Attributer
	// RecentTorrents are the Datastar attributes, from the site's adapter,
	// that list the torrents last added to TorrServer into home's first row
	// (RecentTorrentsSlot). Nil leaves the row out.
	RecentTorrents templ.Attributer
}

type siteKey struct{}

// WithSite marks ctx's pages as site's.
func WithSite(ctx context.Context, site Site) context.Context {
	return context.WithValue(ctx, siteKey{}, site)
}

func siteOf(ctx context.Context) Site {
	site, _ := ctx.Value(siteKey{}).(Site)
	return site
}

// IsCloud reports whether the request is for the hosted web app.
func IsCloud(ctx context.Context) bool { return siteOf(ctx).Cloud }

// pageScripts are a page's module scripts. Every web app page loads
// torrserver.js: its navbar checks the visitor's TorrServer from the browser.
func pageScripts(ctx context.Context, scripts []string) []string {
	if !siteOf(ctx).Cloud || slices.Contains(scripts, torrServerScript) {
		return scripts
	}
	return append([]string{torrServerScript}, scripts...)
}

const torrServerScript = "/static/torrserver.js"

func titleScripts(ctx context.Context) []string {
	if siteOf(ctx).Cloud {
		return []string{"/static/torrserver.js"}
	}
	return nil
}

// cloudLinks are the web app's signed-in destinations.
var cloudLinks = []navLink{
	{Href: "/", Label: "Home", Icon: "house"},
	{Href: "/search", Label: "Search", Icon: "search"},
	{Href: "/favorites", Label: "Favourites", Icon: "heart"},
	{Href: "/torrserver", Label: "TorrServer", Icon: "server"},
}

// navLinks are the navbar's destinations on ctx's site.
func navLinks(ctx context.Context) []navLink {
	if siteOf(ctx).Cloud {
		return cloudLinks
	}
	return appLinks
}

// homeHref is where the brand leads a signed-in user; signInHref is where
// signed-out visitors sign in.
func homeHref(ctx context.Context) string {
	if siteOf(ctx).Cloud {
		return "/"
	}
	return "/movies"
}

func signInHref(ctx context.Context) string {
	if siteOf(ctx).Cloud {
		return "/"
	}
	return "/login"
}
