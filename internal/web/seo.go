package web

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/lieranderl/moviestracker-app/internal/views"
)

// publicPaths are the web app's pages that anyone can open, which search
// engines index; the others are for signed-in users.
var publicPaths = []string{"/", views.PrivacyPath}

// handleRobots tells crawlers to keep to the public pages and where the
// sitemap is.
func (a *app) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	var b strings.Builder
	b.WriteString("User-agent: *\nAllow: /\n")
	for _, p := range []string{"/api/", "/auth/"} {
		fmt.Fprintf(&b, "Disallow: %s\n", p)
	}
	if a.cfg.BaseURL != "" {
		fmt.Fprintf(&b, "\nSitemap: %s/sitemap.xml\n", a.cfg.BaseURL)
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		slog.Warn("writing robots.txt failed", "error", err)
	}
}

// handleSitemap lists the public pages for search engines.
func (a *app) handleSitemap(w http.ResponseWriter, r *http.Request) {
	if a.cfg.BaseURL == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, p := range publicPaths {
		fmt.Fprintf(&b, "  <url><loc>%s%s</loc></url>\n", a.cfg.BaseURL, p)
	}
	b.WriteString("</urlset>\n")
	if _, err := io.WriteString(w, b.String()); err != nil {
		slog.Warn("writing sitemap.xml failed", "error", err)
	}
}
