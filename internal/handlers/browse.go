package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// browseView is the list /browse/{slug} names, with ?window=day for today's
// trending; ok is false for a list there is not.
func (s *Server) browseView(r *http.Request) (views.BrowseView, bool) {
	list, ok := views.FindBrowseList(r.PathValue("slug"))
	if !ok || s.clients().Details == nil {
		return views.BrowseView{}, false
	}
	return views.BrowseView{List: list, Today: list.Day != "" && r.URL.Query().Get("window") == "day"}, true
}

// handleBrowsePage shows a home row whole: its first 20 titles, and more
// as the page scrolls (handleBrowseMore).
func (s *Server) handleBrowsePage(w http.ResponseWriter, r *http.Request) {
	user := s.userFromRequest(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	v, ok := s.browseView(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := s.detailsContext(r)
	defer cancel()
	p, err := s.clients().Details.ListPage(ctx, v.Source(), 1)
	if err != nil {
		slog.Warn("tmdb browse page failed", "list", v.Source(), "error", err)
		v.Failed = true
	} else {
		v.Items = p.Items
		if p.TotalPages > 1 {
			v.Next = 2
		}
	}
	templ.Handler(views.BrowsePage(user, v)).ServeHTTP(w, r)
}

// handleBrowseMore appends ?page= of a list to the grid and moves the
// loader under it to the page after, until TMDB's last page.
func (s *Server) handleBrowseMore(w http.ResponseWriter, r *http.Request) {
	if s.apiUser(w, r) == nil {
		return
	}
	v, ok := s.browseView(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 2 || page > tmdb.MaxPage {
		http.Error(w, "Ask for a page from 2 to 500.", http.StatusBadRequest)
		return
	}
	ctx, cancel := s.detailsContext(r)
	defer cancel()
	details := s.clients().Details
	p, err := details.ListPage(ctx, v.Source(), page)
	sse := datastar.NewSSE(w, r)
	if err != nil {
		slog.Warn("tmdb browse page failed", "list", v.Source(), "page", page, "error", err)
		v.Failed, v.Next = true, page
		if err := sse.PatchElementTempl(views.BrowseMore(v)); err != nil {
			logSSEError(r, "patch browse retry", err)
		}
		return
	}
	// TMDB's pages shift as titles move up and down: what the page before
	// showed is not shown again.
	shown := map[string]bool{}
	if prev, err := details.ListPage(ctx, v.Source(), page-1); err == nil {
		for _, it := range prev.Items {
			shown[it.MediaType+"/"+strconv.Itoa(it.ID)] = true
		}
	}
	for _, it := range p.Items {
		if !shown[it.MediaType+"/"+strconv.Itoa(it.ID)] {
			v.Items = append(v.Items, it)
		}
	}
	if page < p.TotalPages {
		v.Next = page + 1
	}
	if err := sse.PatchElementTempl(views.BrowseItems(v.Items), datastar.WithSelectorID("browse-grid"), datastar.WithModeAppend()); err != nil {
		logSSEError(r, "patch browse page", err)
		return
	}
	if err := sse.PatchElementTempl(views.BrowseMore(v)); err != nil {
		logSSEError(r, "patch browse loader", err)
	}
}
