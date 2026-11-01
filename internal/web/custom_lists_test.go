package web_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAUserMakesAListAndOpensItFromMyLists(t *testing.T) {
	h, session, users := favorites(t)
	res := sendSignals(t, h, http.MethodPost, "/api/lists", `{"listName":"Sci-fi nights"}`, session)
	lists, _ := users.CustomLists(context.Background(), "1098765")
	if res.Code != http.StatusOK || len(lists) != 1 || lists[0].Name != "Sci-fi nights" {
		t.Fatalf("making a list = %d, kept %+v", res.Code, lists)
	}
	href := "/lists/c/" + lists[0].ID
	if !strings.Contains(res.Body.String(), `"redirectUrl":"`+href+`"`) {
		t.Errorf("making a list did not open it:\n%s", res.Body)
	}
	page := html.UnescapeString(getWith(t, h, href, session).Body.String())
	for _, want := range []string{"Sci-fi nights", "This list is empty", "@get('/api/lists/tabs?active=" + lists[0].ID + "')"} {
		if !strings.Contains(page, want) {
			t.Errorf("the new list's page lacks %q", want)
		}
	}
	tabs := html.UnescapeString(send(t, h, http.MethodGet, "/api/lists/tabs?active="+lists[0].ID, session).Body.String())
	if !strings.Contains(tabs, `href="`+href+`" role="tab" aria-selected="true"`) || !strings.Contains(tabs, "Sci-fi nights") {
		t.Errorf("My lists' tabs do not show the list as open:\n%s", tabs)
	}
	if page := html.UnescapeString(getWith(t, h, "/lists/want", session).Body.String()); !strings.Contains(page, "@get('/api/lists/tabs?active=want')") {
		t.Error("Want to watch does not ask for the user's own lists as tabs")
	}
}

func TestAUserAddsAMovieToTheirListFromItsPage(t *testing.T) {
	h, session, users := favorites(t)
	ctx := context.Background()
	l, err := users.CreateCustomList(ctx, "1098765", "Sci-fi nights")
	if err != nil {
		t.Fatal(err)
	}
	if page := html.UnescapeString(getWith(t, h, "/movie/438631", session).Body.String()); !strings.Contains(page, "@get('/api/lists/title?type=movie&id=438631')") {
		t.Error("the movie page does not ask which lists Dune is in")
	}
	picker := html.UnescapeString(send(t, h, http.MethodGet, "/api/lists/title?type=movie&id=438631", session).Body.String())
	if !strings.Contains(picker, "Sci-fi nights") || !strings.Contains(picker, `data-list="`+l.ID+`" aria-checked="false"`) {
		t.Errorf("the picker does not offer the list, unticked:\n%s", picker)
	}
	itemURL := "/api/lists/" + l.ID + "/titles?type=movie&id=438631"
	picker = html.UnescapeString(send(t, h, http.MethodPost, itemURL, session).Body.String())
	if !strings.Contains(picker, `data-list="`+l.ID+`" aria-checked="true"`) {
		t.Errorf("after adding Dune the picker is:\n%s", picker)
	}
	kept, _, _ := users.CustomList(ctx, "1098765", l.ID)
	if len(kept.Titles) != 1 || kept.Titles[0].Title != "Dune" || kept.Titles[0].Poster != "/d.jpg" {
		t.Fatalf("the list keeps %+v, want Dune as TMDB names it", kept.Titles)
	}
	page := html.UnescapeString(getWith(t, h, "/lists/c/"+l.ID, session).Body.String())
	if !strings.Contains(page, `href="/movie/438631"`) || !strings.Contains(page, "@delete('"+itemURL+"&from=list')") {
		t.Errorf("the list page does not show Dune with a way to take it out")
	}
	res := send(t, h, http.MethodDelete, itemURL+"&from=list", session)
	if !strings.Contains(res.Body.String(), "#list-item-movie-438631") {
		t.Errorf("taking Dune out on the list page did not remove its card:\n%s", res.Body)
	}
	if kept, _, _ := users.CustomList(ctx, "1098765", l.ID); len(kept.Titles) != 0 {
		t.Errorf("the list still keeps %+v", kept.Titles)
	}
}

func TestAUserMakesAListForAMovieFromItsPage(t *testing.T) {
	h, session, users := favorites(t)
	res := sendSignals(t, h, http.MethodPost, "/api/lists?type=movie&id=438631", `{"listName":"To show Anna"}`, session)
	lists, _ := users.CustomLists(context.Background(), "1098765")
	if len(lists) != 1 || len(lists[0].Titles) != 1 || lists[0].Titles[0].Title != "Dune" {
		t.Fatalf("making a list from Dune's page kept %+v", lists)
	}
	body := html.UnescapeString(res.Body.String())
	if !strings.Contains(body, `data-list="`+lists[0].ID+`" aria-checked="true"`) || strings.Contains(body, "redirectUrl") {
		t.Errorf("the picker should show the new list ticked, staying on the page:\n%s", body)
	}
}

func TestAUserCannotSeeOrChangeAnotherUsersList(t *testing.T) {
	h, session, users := favorites(t)
	theirs, err := users.CreateCustomList(context.Background(), "someone-else", "Private")
	if err != nil {
		t.Fatal(err)
	}
	if res := getWith(t, h, "/lists/c/"+theirs.ID, session); res.Code != http.StatusNotFound {
		t.Errorf("another user's list page = %d, want 404", res.Code)
	}
	if res := send(t, h, http.MethodPost, "/api/lists/"+theirs.ID+"/titles?type=movie&id=438631", session); res.Code != http.StatusNotFound {
		t.Errorf("adding to another user's list = %d, want 404", res.Code)
	}
	if res := send(t, h, http.MethodDelete, "/api/lists/"+theirs.ID, session); res.Code != http.StatusNotFound {
		t.Errorf("deleting another user's list = %d, want 404", res.Code)
	}
	if kept, _, _ := users.CustomList(context.Background(), "someone-else", theirs.ID); kept.Name != "Private" || len(kept.Titles) != 0 {
		t.Errorf("their list is now %+v", kept)
	}
}

func TestAUserDeletesTheirList(t *testing.T) {
	h, session, users := favorites(t)
	l, err := users.CreateCustomList(context.Background(), "1098765", "Old")
	if err != nil {
		t.Fatal(err)
	}
	page := html.UnescapeString(getWith(t, h, "/lists/c/"+l.ID, session).Body.String())
	if !strings.Contains(page, "@delete('/api/lists/"+l.ID+"')") {
		t.Error("the list page has no way to delete it")
	}
	res := send(t, h, http.MethodDelete, "/api/lists/"+l.ID, session)
	if !strings.Contains(res.Body.String(), `"redirectUrl":"/lists/want"`) {
		t.Errorf("deleting the list did not go back to My lists:\n%s", res.Body)
	}
	if _, ok, _ := users.CustomList(context.Background(), "1098765", l.ID); ok {
		t.Error("the list is still kept")
	}
}

func TestAUserSharesAListByALinkAnyoneCanOpenUntilTheyStop(t *testing.T) {
	h, session, users := favorites(t)
	ctx := context.Background()
	l, err := users.CreateCustomList(ctx, "1098765", "For Anna")
	if err != nil {
		t.Fatal(err)
	}
	send(t, h, http.MethodPost, "/api/lists/"+l.ID+"/titles?type=movie&id=438631", session)
	shareURL := "/api/lists/" + l.ID + "/share"
	if page := html.UnescapeString(getWith(t, h, "/lists/c/"+l.ID, session).Body.String()); !strings.Contains(page, "@post('"+shareURL+"')") {
		t.Error("the list page offers no way to share it")
	}
	body := html.UnescapeString(send(t, h, http.MethodPost, shareURL, session).Body.String())
	kept, _, _ := users.CustomList(ctx, "1098765", l.ID)
	link := "/l/" + kept.ShareID
	if kept.ShareID == "" || !strings.Contains(body, link) || !strings.Contains(body, "@delete('"+shareURL+"')") {
		t.Fatalf("sharing answered without the link %q and a way to stop:\n%s", link, body)
	}
	res := getWith(t, h, link, httptest.NewRecorder())
	page := html.UnescapeString(res.Body.String())
	if res.Code != http.StatusOK || !strings.Contains(page, "For Anna") || !strings.Contains(page, `href="/movie/438631"`) {
		t.Errorf("the shared list, signed out = %d:\n%s", res.Code, page)
	}
	if res.Header().Get("X-Robots-Tag") != "noindex" {
		t.Error("search engines may index a shared list")
	}
	if strings.Contains(page, "@delete(") {
		t.Error("the shared list can be changed by whoever opens it")
	}
	send(t, h, http.MethodDelete, shareURL, session)
	if res := getWith(t, h, link, httptest.NewRecorder()); res.Code != http.StatusNotFound {
		t.Errorf("after sharing stopped the link = %d, want 404", res.Code)
	}
}
