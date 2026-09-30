package views

import "context"

// Site is which app a page belongs to: the local app (the zero value) or
// the cloud web app, whose visitors sign in with Google and use their own
// TorrServer from the browser.
type Site struct {
	Cloud bool
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

// cloudLinks are the web app's signed-in destinations.
var cloudLinks = []navLink{
	{Href: "/", Label: "Home", Icon: "house"},
	{Href: "/search", Label: "Search", Icon: "search"},
	{Href: "/favorites", Label: "Favourites", Icon: "heart"},
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
