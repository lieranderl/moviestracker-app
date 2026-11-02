package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// CustomLists serves the lists users make, for both apps.
type CustomLists struct {
	store   store.CustomListStore
	sharing store.ListSharing // nil: lists are not shared (the local app)
	user    func(*http.Request) (uid string, ok bool)
	catalog *Catalog // names titles as TMDB does
}

// NewCustomLists keeps lists in s for the users user names, shared by
// sharing unless it is nil; catalog names the titles put in them.
func NewCustomLists(s store.CustomListStore, sharing store.ListSharing, user func(*http.Request) (string, bool), catalog *Catalog) *CustomLists {
	return &CustomLists{store: s, sharing: sharing, user: user, catalog: catalog}
}

// Register adds the custom-list routes to mux.
func (cl *CustomLists) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /lists/c/{id}", cl.handlePage)
	mux.HandleFunc("GET /api/lists/tabs", cl.handleTabs)
	mux.HandleFunc("POST /api/lists", cl.handleCreate)
	mux.HandleFunc("DELETE /api/lists/{id}", cl.handleDelete)
	mux.HandleFunc("GET /api/lists/title", cl.handlePicker)
	mux.HandleFunc("POST /api/lists/{id}/titles", cl.handleAdd)
	mux.HandleFunc("DELETE /api/lists/{id}/titles", cl.handleTakeOut)
	if cl.sharing != nil {
		mux.HandleFunc("POST /api/lists/{id}/share", cl.handleShare)
		mux.HandleFunc("DELETE /api/lists/{id}/share", cl.handleShare)
		mux.HandleFunc("GET /l/{share}", cl.handleShared)
	}
}

// handleShare turns the link to one of the user's lists on (POST) or off.
func (cl *CustomLists) handleShare(w http.ResponseWriter, r *http.Request) {
	uid, ok := cl.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	l, err := cl.sharing.ShareList(ctx, uid, r.PathValue("id"), r.Method == http.MethodPost)
	if errors.Is(err, store.ErrNoList) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Warn("sharing a list failed", "error", logError(err))
		http.Error(w, i18n.T(r.Context(), "The change could not be saved. Please try again."), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.ListShare(l.ID, l.ShareID)); err != nil {
		slog.Warn("patching list sharing failed", "error", err)
	}
}

// handleShared is a shared list, read-only, for whoever has its link. It
// names no one, and search engines are asked to leave it out.
func (cl *CustomLists) handleShared(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	l, ok, err := cl.sharing.SharedList(ctx, r.PathValue("share"))
	if err != nil {
		slog.Warn("reading a shared list failed", "error", logError(err))
		http.Error(w, i18n.T(r.Context(), "Your list could not be loaded right now. Please try again in a moment."), http.StatusServiceUnavailable)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.SharedListPage(cl.catalog.userFromRequest(r), l.Name, listItems(l)).Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "page", "shared list", "error", err)
	}
}

// handlePage is My lists with one of the user's lists open.
func (cl *CustomLists) handlePage(w http.ResponseWriter, r *http.Request) {
	uid, ok := cl.user(r)
	if !ok {
		http.Redirect(w, r, cl.catalog.signIn, http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	l, ok, err := cl.store.CustomList(ctx, uid, r.PathValue("id"))
	if err != nil {
		slog.Warn("reading a list failed", "error", logError(err))
		http.Error(w, i18n.T(r.Context(), "Your list could not be loaded right now. Please try again in a moment."), http.StatusServiceUnavailable)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.CustomListPage(cl.catalog.userFromRequest(r), cl.view(r.Context(), l)).Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "page", "custom list", "error", err)
	}
}

// view is l as its page shows it.
// view is the list as its page and patches show it, its titles named in
// ctx's language.
func (cl *CustomLists) view(ctx context.Context, l store.CustomList) views.CustomListView {
	return views.CustomListView{ID: l.ID, Name: l.Name, Items: cl.catalog.LocalizeTitles(ctx, listItems(l)), Sharing: cl.sharing != nil, ShareID: l.ShareID}
}

// listItems are l's titles as cards show them.
func listItems(l store.CustomList) []tmdb.MediaItem {
	items := make([]tmdb.MediaItem, 0, len(l.Titles))
	for _, t := range l.Titles {
		items = append(items, tmdb.MediaItem{ID: t.TMDBID, Title: t.Title, PosterPath: t.Poster, MediaType: t.Kind})
	}
	return items
}

// handleTabs sends My lists the user's lists as tabs, ?active= open. A
// store that does not answer leaves only the tab that makes a list.
func (cl *CustomLists) handleTabs(w http.ResponseWriter, r *http.Request) {
	uid, ok := cl.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	lists, readErr := cl.store.CustomLists(ctx, uid)
	if readErr != nil {
		slog.Warn("reading lists failed", "error", logError(readErr))
	}
	links := make([]views.ListLink, 0, len(lists))
	for _, l := range lists {
		links = append(links, views.ListLink{ID: l.ID, Name: l.Name})
	}
	w.Header().Set("Cache-Control", "no-store")
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.ListTabs(r.URL.Query().Get("active"), links, false)); err != nil {
		slog.Warn("patching list tabs failed", "error", err)
		return
	}
	// The hint is for users who have made no list, not for a store that
	// did not answer.
	if err := sse.PatchElementTempl(views.ListsHint(readErr == nil && len(lists) == 0, cl.sharing != nil)); err != nil {
		slog.Warn("patching the lists hint failed", "error", err)
	}
}

// handleCreate makes a list named $listName and opens it; from a title's
// page (?type=&id=) it puts the title in it and the page stays.
func (cl *CustomLists) handleCreate(w http.ResponseWriter, r *http.Request) {
	uid, ok := cl.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	fromTitle := r.URL.Query().Has("type")
	var kind string
	var id int
	if fromTitle {
		if uid, kind, id, ok = titleTarget(w, r, cl.user); !ok {
			return
		}
	}
	var sig struct {
		ListName string `json:"listName"`
	}
	if err := datastar.ReadSignals(r, &sig); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	l, err := cl.store.CreateCustomList(ctx, uid, sig.ListName)
	if err != nil {
		patchListError(w, r, err)
		return
	}
	if !fromTitle {
		Navigate(w, r, "/lists/c/"+l.ID)
		return
	}
	if !cl.add(w, r, uid, l.ID, kind, id) {
		return
	}
	cl.patchPicker(w, r, uid, kind, id, map[string]any{"listName": ""})
}

// handleDelete deletes one of the user's lists and goes back to My lists.
func (cl *CustomLists) handleDelete(w http.ResponseWriter, r *http.Request) {
	uid, ok := cl.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	err := cl.store.RemoveCustomList(ctx, uid, r.PathValue("id"))
	if errors.Is(err, store.ErrNoList) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Warn("deleting a list failed", "error", logError(err))
		http.Error(w, i18n.T(r.Context(), "The change could not be saved. Please try again."), http.StatusServiceUnavailable)
		return
	}
	Navigate(w, r, "/lists/want")
}

// handlePicker sends a title's page its Add to list menu.
func (cl *CustomLists) handlePicker(w http.ResponseWriter, r *http.Request) {
	uid, kind, id, ok := titleTarget(w, r, cl.user)
	if !ok {
		return
	}
	cl.patchPicker(w, r, uid, kind, id, nil)
}

// patchPicker patches a title's Add to list menu, and signals if any. A
// store that does not answer leaves the menu without lists.
func (cl *CustomLists) patchPicker(w http.ResponseWriter, r *http.Request, uid, kind string, id int, signals map[string]any) {
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	lists, err := cl.store.CustomLists(ctx, uid)
	if err != nil {
		slog.Warn("reading lists failed", "error", logError(err))
	}
	picks := make([]views.PickerList, 0, len(lists))
	for _, l := range lists {
		in := slices.ContainsFunc(l.Titles, func(t store.ListTitle) bool { return t.Kind == kind && t.TMDBID == id })
		picks = append(picks, views.PickerList{ID: l.ID, Name: l.Name, In: in})
	}
	w.Header().Set("Cache-Control", "no-store")
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.ListPicker(kind, id, picks)); err != nil {
		slog.Warn("patching the list picker failed", "error", err)
		return
	}
	if signals != nil {
		if err := sse.MarshalAndPatchSignals(signals); err != nil {
			slog.Warn("patching list signals failed", "error", err)
		}
	}
}

// handleAdd puts a title in one of the user's lists, named and postered as
// TMDB gives it in the user's language.
func (cl *CustomLists) handleAdd(w http.ResponseWriter, r *http.Request) {
	uid, kind, id, ok := titleTarget(w, r, cl.user)
	if !ok || !cl.add(w, r, uid, r.PathValue("id"), kind, id) {
		return
	}
	cl.patchPicker(w, r, uid, kind, id, nil)
}

// add puts a title in list; when it cannot it answers the request itself
// and returns false.
func (cl *CustomLists) add(w http.ResponseWriter, r *http.Request, uid, list, kind string, id int) bool {
	item, err := cl.catalog.Title(r.Context(), kind, id)
	if errors.Is(err, tmdb.ErrNotFound) {
		http.NotFound(w, r)
		return false
	}
	if err != nil {
		slog.Warn("looking up a title on TMDB failed", "error", logError(err))
		http.Error(w, i18n.T(r.Context(), "TMDB is not responding right now. Please try again."), http.StatusBadGateway)
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	err = cl.store.AddToList(ctx, uid, list, store.ListTitle{Kind: kind, TMDBID: id, Title: item.Title, Poster: item.PosterPath})
	if errors.Is(err, store.ErrNoList) {
		http.NotFound(w, r)
		return false
	}
	if err != nil {
		patchListError(w, r, err)
		return false
	}
	return true
}

// handleTakeOut takes a title out of one of the user's lists: on the list's
// page (?from=list) its card goes, on the title's its menu is patched.
func (cl *CustomLists) handleTakeOut(w http.ResponseWriter, r *http.Request) {
	uid, kind, id, ok := titleTarget(w, r, cl.user)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	err := cl.store.RemoveFromList(ctx, uid, r.PathValue("id"), kind, id)
	if errors.Is(err, store.ErrNoList) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Warn("taking a title out of a list failed", "error", logError(err))
		http.Error(w, i18n.T(r.Context(), "The change could not be saved. Please try again."), http.StatusServiceUnavailable)
		return
	}
	if r.URL.Query().Get("from") == "list" {
		cl.patchSection(w, r, uid, r.PathValue("id"))
		return
	}
	cl.patchPicker(w, r, uid, kind, id, nil)
}

// patchSection patches a list's page with the list as it is now: its count
// and, once empty, that it is.
func (cl *CustomLists) patchSection(w http.ResponseWriter, r *http.Request, uid, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	l, ok, err := cl.store.CustomList(ctx, uid, id)
	if err != nil || !ok {
		slog.Warn("reading a list failed", "error", logError(err))
		Navigate(w, r, "/lists/c/"+id)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.CustomListSection(cl.view(r.Context(), l))); err != nil {
		slog.Warn("patching a list failed", "error", err)
	}
}

// patchListError says in the page why a list could not be made or filled.
func patchListError(w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.NewListError(listError(r.Context(), err))); err != nil {
		slog.Warn("patching a list error failed", "error", err)
	}
}

// listError is why a list could not be made or changed, in words.
func listError(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, store.ErrTooManyLists):
		return i18n.T(ctx, "You have 50 lists already. Delete one to make another.")
	case errors.Is(err, store.ErrListFull):
		return i18n.T(ctx, "This list has 500 titles already.")
	case errors.Is(err, store.ErrNoList):
		return i18n.T(ctx, "This list is gone.")
	case errors.Is(err, store.ErrListName):
		return i18n.T(ctx, "Give the list a name of up to 60 characters.")
	default:
		slog.Warn("changing a list failed", "error", logError(err))
		return i18n.T(ctx, "The change could not be saved. Please try again.")
	}
}
