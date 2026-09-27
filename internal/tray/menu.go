package tray

import (
	"net"
	"strconv"
)

// ItemID names a tray menu item.
type ItemID string

// The tray menu, in order.
const (
	VersionLine  ItemID = "version"
	StatusLine   ItemID = "status"
	NewRelease   ItemID = "release"
	Open         ItemID = "open"
	Dashboard    ItemID = "dashboard"
	CopyLAN      ItemID = "lan"
	StartAtLogin ItemID = "login"
	ShowLogs     ItemID = "logs"
	Restart      ItemID = "restart"
	Uninstall    ItemID = "uninstall"
	Quit         ItemID = "quit"
)

// Item is one line of the tray menu. The menu always has the same items;
// Hidden ones are not shown.
type Item struct {
	ID       ItemID
	Title    string
	Disabled bool
	Checked  bool
	Hidden   bool
}

// MenuState is what the menu shows.
type MenuState struct {
	Version      string
	Status       Status
	Port         int
	LAN          string // this computer's address on the local network, if any
	StartAtLogin bool
	Release      *Release // a newer Moviestracker, if one is out
}

// Menu is the tray menu for st: the same items the macOS menu bar app has,
// except GStreamer, which Windows' TorrServer carries.
func Menu(st MenuState) []Item {
	var status string
	switch st.Status.State {
	case Starting:
		status = "Moviestracker is starting…"
	case Running:
		status = "Moviestracker is running"
	case OtherInstance:
		status = "Another Moviestracker is using port " + strconv.Itoa(st.Port)
	default:
		status = "Moviestracker " + st.Status.Reason
		if st.Status.Reason == "" {
			status = "Moviestracker is stopped"
		}
	}
	up := st.Status.State == Running || st.Status.State == OtherInstance
	return []Item{
		{ID: VersionLine, Title: "Moviestracker " + st.Version, Disabled: true},
		{ID: StatusLine, Title: status, Disabled: true},
		newRelease(st.Release),
		{ID: Open, Title: "Open Moviestracker", Disabled: !up},
		{ID: Dashboard, Title: "Dashboard", Disabled: !up},
		{ID: CopyLAN, Title: "On a TV or phone: " + LANURL(st.LAN, st.Port) + " (click to copy)", Hidden: st.LAN == ""},
		{ID: StartAtLogin, Title: "Start when I sign in", Checked: st.StartAtLogin},
		{ID: ShowLogs, Title: "Show Logs"},
		{ID: Restart, Title: "Restart"},
		{ID: Uninstall, Title: "Uninstall Moviestracker…"},
		{ID: Quit, Title: "Quit Moviestracker"},
	}
}

// newRelease is the item that downloads a newer Moviestracker; hidden
// without one.
func newRelease(rel *Release) Item {
	if rel == nil {
		return Item{ID: NewRelease, Title: "Download a new Moviestracker…", Hidden: true}
	}
	return Item{ID: NewRelease, Title: "Download Moviestracker " + rel.Version + "…"}
}

// LANURL is Moviestracker's address for other devices on the network.
func LANURL(ip string, port int) string {
	return "http://" + net.JoinHostPort(ip, strconv.Itoa(port))
}

// LANAddress is this computer's private IPv4 address on the network its
// default route uses (Wi-Fi or Ethernet rather than a VPN or virtual
// adapter), or "" without one. Nothing is sent: a UDP "connection" only
// picks the route.
func LANAddress() string {
	conn, err := net.Dial("udp4", "192.0.2.1:9") // TEST-NET-1: routed like the internet, never reached
	if err != nil {
		return ""
	}
	defer func() { _ = conn.Close() }()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || !addr.IP.IsPrivate() {
		return ""
	}
	return addr.IP.String()
}

// MenuItem is a tray menu item as the tray library has it.
type MenuItem interface {
	SetTitle(string)
	Enable()
	Disable()
	Check()
	Uncheck()
	Hide()
	Show()
}

// Apply shows items in the menu; get returns the tray's item for an ID.
// Hiding comes last: on Windows, changing an item's title, state or check
// mark puts it back in the menu, even after Hide.
func Apply(items []Item, get func(ItemID) MenuItem) {
	for _, it := range items {
		m := get(it.ID)
		m.SetTitle(it.Title)
		if it.Disabled {
			m.Disable()
		} else {
			m.Enable()
		}
		if it.Checked {
			m.Check()
		} else {
			m.Uncheck()
		}
		if it.Hidden {
			m.Hide()
		} else {
			m.Show()
		}
	}
}
