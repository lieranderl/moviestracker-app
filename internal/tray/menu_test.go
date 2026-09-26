package tray_test

import (
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tray"
)

func find(t *testing.T, items []tray.Item, id tray.ItemID) tray.Item {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("the menu has no %q item", id)
	return tray.Item{}
}

func TestTheMenuSaysWhatMoviestrackerIsDoing(t *testing.T) {
	for _, tc := range []struct {
		status tray.Status
		want   string
	}{
		{tray.Status{State: tray.Starting}, "Moviestracker is starting…"},
		{tray.Status{State: tray.Running}, "Moviestracker is running"},
		{tray.Status{State: tray.OtherInstance}, "Another Moviestracker is using port 8095"},
		{tray.Status{State: tray.Stopped, Reason: "stopped (exit status 1); starting again in 2s"}, "Moviestracker stopped (exit status 1); starting again in 2s"},
	} {
		items := tray.Menu(tray.MenuState{Version: "v1.2.3", Status: tc.status, Port: 8095})
		if got := find(t, items, tray.StatusLine); got.Title != tc.want || !got.Disabled {
			t.Errorf("status %+v reads %+v, want the disabled line %q", tc.status, got, tc.want)
		}
		if got := find(t, items, tray.VersionLine).Title; got != "Moviestracker v1.2.3" {
			t.Errorf("version line = %q", got)
		}
	}
}

func TestTheMenuGivesTheAddressForTVsAndPhones(t *testing.T) {
	items := tray.Menu(tray.MenuState{Status: tray.Status{State: tray.Running}, Port: 8095, LAN: "192.168.1.20"})
	if got := find(t, items, tray.CopyLAN); got.Hidden || got.Title != "On a TV or phone: http://192.168.1.20:8095 (click to copy)" {
		t.Errorf("LAN item = %+v", got)
	}
	items = tray.Menu(tray.MenuState{Status: tray.Status{State: tray.Running}, Port: 8095})
	if got := find(t, items, tray.CopyLAN); !got.Hidden {
		t.Errorf("without a network address the LAN item shows: %+v", got)
	}
}

func TestTheMenuShowsWhetherMoviestrackerStartsAtSignIn(t *testing.T) {
	on := find(t, tray.Menu(tray.MenuState{StartAtLogin: true}), tray.StartAtLogin)
	off := find(t, tray.Menu(tray.MenuState{}), tray.StartAtLogin)
	if !on.Checked || off.Checked || on.Title != "Start when I sign in" {
		t.Errorf("start at sign-in items: on %+v, off %+v", on, off)
	}
}

func TestOpeningMoviestrackerWaitsUntilItRuns(t *testing.T) {
	for state, enabled := range map[tray.State]bool{
		tray.Running: true, tray.OtherInstance: true, tray.Starting: false, tray.Stopped: false,
	} {
		items := tray.Menu(tray.MenuState{Status: tray.Status{State: state}})
		for _, id := range []tray.ItemID{tray.Open, tray.Dashboard} {
			if got := find(t, items, id); got.Disabled == enabled {
				t.Errorf("%q while %s: disabled=%v, want %v", id, state, got.Disabled, !enabled)
			}
		}
	}
}
