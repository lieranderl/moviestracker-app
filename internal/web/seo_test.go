package web_test

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/web"
)

const siteURL = "https://moviestracker.net"

func TestTheHomePageDescribesMoviestrackerToSearchEnginesAndLinkPreviews(t *testing.T) {
	body := html.UnescapeString(get(t, web.New(web.Config{BaseURL: siteURL}), "/").Body.String())
	for _, want := range []string{
		`<meta name="description" content="Browse trending movies and series, find their torrent releases with quality, size and seeders, and watch them on your own TorrServer.`,
		`<link rel="canonical" href="https://moviestracker.net/">`,
		`<meta property="og:url" content="https://moviestracker.net/">`,
		`<meta property="og:site_name" content="Moviestracker">`,
		`type="application/ld+json"`, `"@type":"WebApplication"`, `"url":"https://moviestracker.net/"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home page lacks %s", want)
		}
	}
	if strings.Contains(body, "noindex") {
		t.Error("home page asks search engines not to index it")
	}
}

func TestThePrivacyPolicyHasItsCanonicalAddress(t *testing.T) {
	body := get(t, web.New(web.Config{BaseURL: siteURL}), "/privacy").Body.String()
	if !strings.Contains(body, `<link rel="canonical" href="https://moviestracker.net/privacy">`) {
		t.Error("privacy policy lacks its canonical address")
	}
}

func TestSearchEnginesAreToldWhichPagesToCrawl(t *testing.T) {
	h := web.New(web.Config{BaseURL: siteURL})

	robots := get(t, h, "/robots.txt")
	if robots.Code != http.StatusOK || !strings.HasPrefix(robots.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("GET /robots.txt = %d %q, want 200 text/plain", robots.Code, robots.Header().Get("Content-Type"))
	}
	for _, want := range []string{"User-agent: *", "Disallow: /api/", "Disallow: /auth/", "Sitemap: https://moviestracker.net/sitemap.xml"} {
		if !strings.Contains(robots.Body.String(), want) {
			t.Errorf("robots.txt lacks %q", want)
		}
	}

	sitemap := get(t, h, "/sitemap.xml")
	if sitemap.Code != http.StatusOK || !strings.HasPrefix(sitemap.Header().Get("Content-Type"), "application/xml") {
		t.Fatalf("GET /sitemap.xml = %d %q, want 200 application/xml", sitemap.Code, sitemap.Header().Get("Content-Type"))
	}
	for _, want := range []string{"<loc>https://moviestracker.net/</loc>", "<loc>https://moviestracker.net/privacy</loc>"} {
		if !strings.Contains(sitemap.Body.String(), want) {
			t.Errorf("sitemap.xml lacks %s", want)
		}
	}
}
