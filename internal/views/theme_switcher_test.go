package views_test

import (
	"bytes"
	"context"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

func TestThemeSwitcherRendering(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	err := views.ThemeSwitcher().Render(ctx, &buf)
	if err != nil {
		t.Fatalf("ThemeSwitcher render failed: %v", err)
	}

	out := html.UnescapeString(buf.String())

	// Verify theme options are present
	requiredSnippets := []string{
		`aria-label="Theme switcher"`,
		`class="lucide shrink-0 size-4 text-warning"`,
		`class="lucide shrink-0 size-4 text-primary"`,
		`class="lucide shrink-0 size-4 text-accent"`,
		`$theme = 'light'`,
		`$theme = 'dark'`,
		`$theme = 'system'`,
		`Light`,
		`Dark`,
		`System`,
		`auto`,
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(out, snippet) {
			t.Errorf("ThemeSwitcher missing expected snippet: %q", snippet)
		}
	}
}

func TestLayoutThemeSignals(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	err := views.Layout("Test Title").Render(ctx, &buf)
	if err != nil {
		t.Fatalf("Layout render failed: %v", err)
	}

	out := html.UnescapeString(buf.String())

	// Layout should not hardcode data-theme on <html>
	if strings.Contains(out, `data-theme="`) {
		t.Errorf("Layout should not hardcode data-theme on <html> tag to allow system theme detection")
	}

	// Layout should have theme signal and effect
	if !strings.Contains(out, "theme:") {
		t.Errorf("Layout missing theme in data-signals")
	}
	if !strings.Contains(out, "$theme === 'system'") {
		t.Errorf("Layout missing system theme logic in data-effect")
	}
}

func TestNavbarIncludesThemeSwitcher(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	err := views.Navbar(nil).Render(ctx, &buf)
	if err != nil {
		t.Fatalf("Navbar render failed: %v", err)
	}

	html := buf.String()

	if !strings.Contains(html, `aria-label="Theme switcher"`) {
		t.Errorf("Navbar should include ThemeSwitcher component")
	}
}

func TestLoginIncludesThemeSwitcher(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	err := views.Login(false, "").Render(ctx, &buf)
	if err != nil {
		t.Fatalf("Login render failed: %v", err)
	}

	html := buf.String()

	if !strings.Contains(html, `aria-label="Theme switcher"`) {
		t.Errorf("Login should include ThemeSwitcher component")
	}
}

func TestTheNavbarChecksTorrServerTheWayItsSiteSays(t *testing.T) {
	user := &auth.User{Name: "Ann", Username: "ann"}
	check := templ.OrderedAttributes{{Key: "data-init", Value: "@get('/check')"}}
	var buf bytes.Buffer
	if err := views.Navbar(user).Render(views.WithSite(context.Background(), views.Site{TorrServerCheck: check}), &buf); err != nil {
		t.Fatal(err)
	}
	out := html.UnescapeString(buf.String())
	for _, want := range []string{`id="ts-nav-status"`, `data-init="@get('/check')"`} {
		if !strings.Contains(out, want) {
			t.Errorf("the navbar lacks %q", want)
		}
	}
	buf.Reset()
	if err := views.Navbar(user).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), `id="ts-nav-status"`) {
		t.Error("a site that gives no way to check TorrServer shows no status")
	}
}

func TestPhonesGetATabBarOfTheSignedInDestinations(t *testing.T) {
	user := &auth.User{Name: "Ann", Username: "ann"}
	for _, c := range []struct {
		site  views.Site
		hrefs []string
	}{
		{views.Site{Cloud: true}, []string{`href="/"`, `href="/search"`, `href="/favorites"`, `href="/torrserver"`}},
		{views.Site{}, []string{`href="/movies"`, `href="/search"`, `href="/dashboard"`, `href="/torrserver"`}},
	} {
		var buf bytes.Buffer
		if err := views.Navbar(user).Render(views.WithSite(context.Background(), c.site), &buf); err != nil {
			t.Fatal(err)
		}
		out := html.UnescapeString(buf.String())
		dock := regexp.MustCompile(`(?s)<nav[^>]*class="dock[^"]*md:hidden[^"]*"[^>]*>.*?</nav>`).FindString(out)
		if dock == "" {
			t.Fatalf("cloud=%t: no tab bar hidden from wide screens", c.site.Cloud)
		}
		for _, href := range c.hrefs {
			if !strings.Contains(dock, href) {
				t.Errorf("cloud=%t: the tab bar lacks %s", c.site.Cloud, href)
			}
		}
		if !strings.Contains(dock, "dock-active") || !strings.Contains(dock, "data-attr:aria-current") {
			t.Errorf("cloud=%t: the tab bar does not mark the page it is on", c.site.Cloud)
		}
	}
	var buf bytes.Buffer
	if err := views.Navbar(nil).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), `class="dock`) {
		t.Error("a signed-out visitor has no destinations for a tab bar")
	}
}

func TestPagesMakeRoomForThePhonesTabBar(t *testing.T) {
	var buf bytes.Buffer
	if err := views.Layout("Test").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "max-md:has-[.dock]:pb-16") {
		t.Error("the body does not leave room at the bottom for the tab bar")
	}
}
