package tray_test

import (
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tray"
)

// windowsItem behaves like a Windows tray menu item from fyne.io/systray:
// changing its title, state or check mark puts it back in the menu, even
// after Hide.
type windowsItem struct {
	title           string
	shown, disabled bool
	checked         bool
}

func (w *windowsItem) SetTitle(t string) { w.title = t; w.shown = true }
func (w *windowsItem) Enable()           { w.disabled = false; w.shown = true }
func (w *windowsItem) Disable()          { w.disabled = true; w.shown = true }
func (w *windowsItem) Check()            { w.checked = true; w.shown = true }
func (w *windowsItem) Uncheck()          { w.checked = false; w.shown = true }
func (w *windowsItem) Hide()             { w.shown = false }
func (w *windowsItem) Show()             { w.shown = true }

func TestHiddenMenuItemsStayHiddenOnWindows(t *testing.T) {
	items := map[tray.ItemID]*windowsItem{}
	menu := tray.Menu(tray.MenuState{Version: "v0.4.0", Status: tray.Status{State: tray.Running}, Port: 8095})
	for _, it := range menu {
		items[it.ID] = &windowsItem{shown: true}
	}

	tray.Apply(menu, func(id tray.ItemID) tray.MenuItem { return items[id] })

	for _, id := range []tray.ItemID{tray.NewRelease, tray.CopyLAN} {
		if items[id].shown {
			t.Errorf("the hidden %q item shows: %+v", id, *items[id])
		}
	}
	if open := items[tray.Open]; !open.shown || open.disabled || open.title != "Open Moviestracker" {
		t.Errorf("Open Moviestracker = %+v, want shown and enabled", *open)
	}
}

func TestAMenuItemShowsAgainWhenItHasSomethingToSay(t *testing.T) {
	item := &windowsItem{}
	get := func(tray.ItemID) tray.MenuItem { return item }
	rel := &tray.Release{Version: "v0.5.0", Notes: "https://github.com/lieranderl/moviestracker-app/releases/tag/v0.5.0"}

	tray.Apply([]tray.Item{find(t, tray.Menu(tray.MenuState{}), tray.NewRelease)}, get)
	tray.Apply([]tray.Item{find(t, tray.Menu(tray.MenuState{Release: rel}), tray.NewRelease)}, get)

	if !item.shown || item.title != "Download Moviestracker v0.5.0…" {
		t.Errorf("new release item = %+v, want it shown for v0.5.0", *item)
	}
}
