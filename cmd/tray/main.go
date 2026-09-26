//go:build windows

// Moviestracker.exe is the Windows tray app: it runs moviestracker-server
// (which runs TorrServer) while the user is signed in, and offers what a
// person needs around it: open it, its address for TVs and phones, start at
// sign-in, logs, restart, uninstall. The macOS menu bar app does the same.
//
//	Moviestracker.exe         run, or open the one already running
//	Moviestracker.exe --quit  stop the running one (the installer uses it)
package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/lieranderl/moviestracker-app/internal/tray"
)

// version is set by release builds: -ldflags "-X main.version=v1.2.3".
var version = "dev"

//go:embed moviestracker.ico
var icon []byte

const (
	port     = 8095
	localURL = "http://localhost:8095"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--quit" {
		os.Exit(quitRunning(30 * time.Second))
	}
	dataDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "Moviestracker")
	_ = os.MkdirAll(dataDir, 0o700) // #nosec G703 -- the user's own LOCALAPPDATA
	release, running := claimInstance()
	if running {
		tray.Logf(dataDir, "another Moviestracker tray runs: opening it instead")
		shellOpen(localURL)
		return
	}
	defer release()
	a := &app{dataDir: dataDir}

	exe, err := os.Executable()
	if err != nil {
		fatal("Moviestracker cannot find where it is installed: " + err.Error())
	}
	a.exe = exe
	a.server = tray.NewServer(tray.Config{
		Program:  filepath.Join(filepath.Dir(exe), "moviestracker-server.exe"),
		DataDir:  dataDir,
		Port:     port,
		OnChange: func(tray.Status) { a.refresh() },
	})
	tray.Logf(dataDir, "Moviestracker %s starting on Windows from %s", version, exe)
	systray.Run(a.ready, a.exit)
}

type app struct {
	dataDir, exe string
	server       *tray.Server

	mu       sync.Mutex
	items    map[tray.ItemID]*systray.MenuItem
	lan      string
	quitting sync.Once
}

func (a *app) ready() {
	systray.SetIcon(icon)
	systray.SetTooltip("Moviestracker")
	a.mu.Lock()
	a.items = map[tray.ItemID]*systray.MenuItem{}
	for _, it := range tray.Menu(a.state()) {
		var m *systray.MenuItem
		if it.ID == tray.StartAtLogin {
			m = systray.AddMenuItemCheckbox(it.Title, "", it.Checked)
		} else {
			m = systray.AddMenuItem(it.Title, "")
		}
		a.items[it.ID] = m
		go func(id tray.ItemID) {
			for range m.ClickedCh {
				a.click(id)
			}
		}(it.ID)
		switch it.ID {
		case tray.StatusLine, tray.CopyLAN, tray.Restart:
			systray.AddSeparator()
		}
	}
	a.mu.Unlock()

	go a.whenAskedToQuit()
	go func() {
		// The network address changes when the computer changes networks.
		for ; ; time.Sleep(30 * time.Second) {
			lan := tray.LANAddress()
			a.mu.Lock()
			changed := lan != a.lan
			a.lan = lan
			a.mu.Unlock()
			if changed {
				a.refresh()
			}
		}
	}()
	a.server.Start()
	a.refresh()
}

// state is what the menu shows now.
func (a *app) state() tray.MenuState {
	a.mu.Lock()
	lan := a.lan
	a.mu.Unlock()
	return tray.MenuState{Version: version, Status: a.server.Status(), Port: port, LAN: lan, StartAtLogin: startsAtSignIn(a.exe)}
}

// refresh shows the current state in the menu, and opens Moviestracker in
// the browser the first time it runs, for its setup page.
func (a *app) refresh() {
	st := a.state()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.items == nil {
		return
	}
	for _, it := range tray.Menu(st) {
		m := a.items[it.ID]
		m.SetTitle(it.Title)
		if it.Disabled {
			m.Disable()
		} else {
			m.Enable()
		}
		if it.Hidden {
			m.Hide()
		} else {
			m.Show()
		}
		if it.Checked {
			m.Check()
		} else {
			m.Uncheck()
		}
	}
	systray.SetTooltip(tray.Menu(st)[1].Title)
	if st.Status.State == tray.Running && firstRun(a.dataDir) {
		shellOpen(localURL)
	}
}

func (a *app) click(id tray.ItemID) {
	switch id {
	case tray.Open:
		shellOpen(localURL)
	case tray.Dashboard:
		shellOpen(localURL + "/dashboard")
	case tray.CopyLAN:
		a.mu.Lock()
		lan := a.lan
		a.mu.Unlock()
		if err := copyText(tray.LANURL(lan, port)); err != nil {
			tray.Logf(a.dataDir, "cannot copy the address: %v", err)
		}
	case tray.StartAtLogin:
		if err := setStartAtSignIn(a.exe, !startsAtSignIn(a.exe)); err != nil {
			message("Start when I sign in could not be changed", err.Error())
		}
		a.refresh()
	case tray.ShowLogs:
		tray.Logf(a.dataDir, "showing the log")
		shellOpen(tray.LogPath(a.dataDir))
	case tray.Restart:
		tray.Logf(a.dataDir, "restarting")
		go a.server.Restart()
	case tray.Uninstall:
		a.uninstall()
	case tray.Quit:
		a.quit()
	}
}

// uninstall runs the uninstaller, which asks first and then stops this app
// (Moviestracker.exe --quit) before it removes anything.
func (a *app) uninstall() {
	found, _ := filepath.Glob(filepath.Join(filepath.Dir(a.exe), "unins*.exe"))
	if len(found) == 0 {
		message("Uninstall Moviestracker", "Remove Moviestracker in Settings → Apps → Installed apps.")
		return
	}
	shellOpen(found[0])
}

func (a *app) quit() {
	a.quitting.Do(func() {
		tray.Logf(a.dataDir, "quitting")
		a.server.Stop()
		systray.Quit()
	})
}

// exit runs when the tray ends, also when Windows signs the user out.
func (a *app) exit() { a.server.Stop() }

// firstRun reports, once per data directory, that Moviestracker runs for
// the first time.
func firstRun(dataDir string) bool {
	marker := filepath.Join(dataDir, "tray-opened")
	if _, err := os.Stat(marker); err == nil {
		return false
	}
	return os.WriteFile(marker, []byte("Moviestracker opened the browser once.\n"), 0o600) == nil
}
