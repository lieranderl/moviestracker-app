// Moviestracker.app: a menu bar app that runs the Moviestracker server (which
// runs TorrServer) while the user is logged in, and offers the few things a
// person needs around it: open it, see its address for TVs and phones, start
// at login, logs, GStreamer, uninstall.
import AppKit
import ServiceManagement

let port = 8095
let home = FileManager.default.homeDirectoryForCurrentUser
let dataDir = home.appendingPathComponent("Library/Application Support/moviestracker")
let logURL = dataDir.appendingPathComponent("moviestracker.log")
// GStreamer the app downloads: <gstRoot>/<version>, run from there.
let gstRoot = dataDir.appendingPathComponent("gstreamer")
let localURL = URL(string: "http://localhost:\(port)")!
let appVersion = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "dev"
// No caches or cookies in ~/Library for a health check.
let session = URLSession(configuration: .ephemeral)

// MARK: - Server

/// Server runs the bundled moviestracker program and restarts it when it
/// stops unexpectedly. The program exits by itself when this app is gone.
final class Server {
    enum State: Equatable {
        case starting, running, otherInstance
        case stopped(String)
    }

    private(set) var state: State = .starting { didSet { if state != oldValue { onChange() } } }
    var onChange: () -> Void = {}
    private var process: Process?
    private var stopping = false
    private var backoff: TimeInterval = 2
    private var poll: Timer?

    func start() {
        stopping = false
        healthy { up in
            if up {
                // Someone else (make dev, an older install) already serves the port.
                appLog("port \(port) already answers: another Moviestracker runs")
                self.state = .otherInstance
                self.watch()
                return
            }
            self.launch()
        }
    }

    private func launch() {
        guard let exe = Bundle.main.url(forAuxiliaryExecutable: "moviestracker-server") else {
            state = .stopped("the app is damaged: moviestracker-server is missing")
            return
        }
        try? FileManager.default.createDirectory(at: dataDir, withIntermediateDirectories: true,
                                                 attributes: [.posixPermissions: 0o700])
        rotateLog()
        let p = Process()
        p.executableURL = exe
        var env = ProcessInfo.processInfo.environment
        env["MT_LISTEN"] = ":\(port)"
        env["MT_DATA_DIR"] = dataDir.path
        env["MT_PARENT_PID"] = String(getpid())
        p.environment = env
        if let log = logHandle() {
            p.standardOutput = log
            p.standardError = log
        }
        p.terminationHandler = { [weak self] proc in
            DispatchQueue.main.async { self?.exited(proc) }
        }
        do {
            try p.run()
        } catch {
            state = .stopped(error.localizedDescription)
            return
        }
        process = p
        state = .starting
        watch()
    }

    private func exited(_ p: Process) {
        guard p === process else { return }
        process = nil
        if stopping { return }
        let wait = backoff
        backoff = min(backoff * 2, 60)
        state = .stopped("stopped (exit \(p.terminationStatus)); starting again in \(Int(wait)) s")
        appLog("moviestracker exited with \(p.terminationStatus); restarting in \(Int(wait)) s")
        DispatchQueue.main.asyncAfter(deadline: .now() + wait) { [weak self] in
            guard let self, !self.stopping, self.process == nil else { return }
            self.start()
        }
    }

    /// watch polls /healthz: it tells starting from running, and notices when
    /// another instance goes away so this one can take over.
    private func watch() {
        poll?.invalidate()
        poll = Timer.scheduledTimer(withTimeInterval: 3, repeats: true) { [weak self] _ in
            guard let self else { return }
            self.healthy { up in
                switch self.state {
                case .starting where up:
                    self.state = .running
                    self.backoff = 2
                case .running where !up && self.process != nil:
                    self.state = .starting
                case .otherInstance where !up:
                    self.poll?.invalidate()
                    self.start()
                default:
                    break
                }
            }
        }
    }

    /// stop ends the program: SIGTERM, then SIGKILL after 15 s.
    func stop(then done: @escaping () -> Void) {
        stopping = true
        poll?.invalidate()
        guard let p = process, p.isRunning else {
            process = nil
            done()
            return
        }
        p.terminate()
        DispatchQueue.global().async {
            let deadline = Date().addingTimeInterval(15)
            while p.isRunning && Date() < deadline { usleep(100_000) }
            if p.isRunning { kill(p.processIdentifier, SIGKILL) }
            p.waitUntilExit()
            DispatchQueue.main.async {
                self.process = nil
                self.state = .stopped("stopped")
                done()
            }
        }
    }

    func restart() {
        stop { self.backoff = 2; self.start() }
    }

    private func healthy(_ done: @escaping (Bool) -> Void) {
        var req = URLRequest(url: localURL.appendingPathComponent("healthz"))
        req.timeoutInterval = 2
        session.dataTask(with: req) { data, resp, _ in
            let ok = (resp as? HTTPURLResponse)?.statusCode == 200 && data.map { String(decoding: $0, as: UTF8.self).hasPrefix("ok") } == true
            DispatchQueue.main.async { done(ok) }
        }.resume()
    }
}

// MARK: - Log

func logHandle() -> FileHandle? {
    if !FileManager.default.fileExists(atPath: logURL.path) {
        FileManager.default.createFile(atPath: logURL.path, contents: nil, attributes: [.posixPermissions: 0o600])
    }
    guard let h = try? FileHandle(forWritingTo: logURL) else { return nil }
    h.seekToEndOfFile()
    return h
}

/// rotateLog keeps one previous log once the current one passes 10 MB. It
/// copies and truncates, since the server keeps the log open (appending).
func rotateLog() {
    guard let size = (try? FileManager.default.attributesOfItem(atPath: logURL.path))?[.size] as? Int,
          size > 10 << 20 else { return }
    let old = dataDir.appendingPathComponent("moviestracker.log.1")
    try? FileManager.default.removeItem(at: old)
    try? FileManager.default.copyItem(at: logURL, to: old)
    if let h = try? FileHandle(forWritingTo: logURL) {
        try? h.truncate(atOffset: 0)
        try? h.close()
    }
}

func appLog(_ message: String) {
    guard let h = logHandle() else { return }
    h.write(Data("app: \(message)\n".utf8))
    try? h.close()
}

// MARK: - Machine

/// lanAddress is this Mac's address on the local network, for TVs and phones.
func lanAddress() -> String? {
    var ifaddr: UnsafeMutablePointer<ifaddrs>?
    guard getifaddrs(&ifaddr) == 0, let first = ifaddr else { return nil }
    defer { freeifaddrs(ifaddr) }
    var best: String?
    for ptr in sequence(first: first, next: { $0.pointee.ifa_next }) {
        let ifa = ptr.pointee
        guard let addr = ifa.ifa_addr, addr.pointee.sa_family == UInt8(AF_INET),
              ifa.ifa_flags & UInt32(IFF_UP) != 0, ifa.ifa_flags & UInt32(IFF_LOOPBACK) == 0 else { continue }
        var host = [CChar](repeating: 0, count: Int(NI_MAXHOST))
        guard getnameinfo(addr, socklen_t(addr.pointee.sa_len), &host, socklen_t(host.count), nil, 0, NI_NUMERICHOST) == 0 else { continue }
        let ip = String(cString: host)
        let name = String(cString: ifa.ifa_name)
        let privateLAN = ip.hasPrefix("192.168.") || ip.hasPrefix("10.") || ip.range(of: #"^172\.(1[6-9]|2\d|3[01])\."#, options: .regularExpression) != nil
        guard privateLAN else { continue }
        if name.hasPrefix("en") { return ip } // Wi-Fi or Ethernet
        best = best ?? ip
    }
    return best
}

/// appGStreamer is the GStreamer this app downloaded, if any.
func appGStreamer() -> (version: String, dir: URL)? {
    guard let names = try? FileManager.default.contentsOfDirectory(atPath: gstRoot.path) else { return nil }
    for name in names.sorted().reversed() where !name.hasPrefix(".") {
        let dir = gstRoot.appendingPathComponent(name)
        if FileManager.default.fileExists(atPath: dir.appendingPathComponent("lib/libgstreamer-1.0.0.dylib").path) {
            return (name, dir)
        }
    }
    return nil
}

/// gstreamerVersion is the version of the GStreamer TorrServer will use:
/// the app's own download, else one installed with Homebrew or GStreamer's
/// installer. nil when there is none.
func gstreamerVersion() -> String? {
    if let gst = appGStreamer() { return gst.version }
    for dir in ["/opt/homebrew/bin", "/usr/local/bin", "/Library/Frameworks/GStreamer.framework/Commands"] {
        let tool = dir + "/gst-inspect-1.0"
        guard FileManager.default.isExecutableFile(atPath: tool) else { continue }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: tool)
        p.arguments = ["--version"]
        p.environment = ["GST_REGISTRY_UPDATE": "no"]
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        guard (try? p.run()) != nil else { continue }
        let text = String(decoding: out.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
        p.waitUntilExit()
        for line in text.split(separator: "\n") where line.hasPrefix("GStreamer ") {
            return String(line.dropFirst("GStreamer ".count))
        }
    }
    return nil
}

func newEnough(_ version: String) -> Bool {
    let parts = version.split(separator: ".").compactMap { Int($0) }
    guard parts.count >= 2 else { return false }
    return parts[0] > 1 || (parts[0] == 1 && parts[1] >= 22)
}

// MARK: - Where the app runs

/// runningFromDownload reports whether the app runs from a disk image or a
/// read-only copy macOS made of a download (App Translocation): a login item
/// would point there and the next start would fail.
var runningFromDownload: Bool {
    let path = Bundle.main.bundlePath
    return path.hasPrefix("/Volumes/") || path.contains("/AppTranslocation/")
}

/// moveToApplications copies the app to /Applications and opens that copy.
func moveToApplications() -> Bool {
    let target = URL(fileURLWithPath: "/Applications/Moviestracker.app")
    do {
        if FileManager.default.fileExists(atPath: target.path) {
            try FileManager.default.trashItem(at: target, resultingItemURL: nil)
        }
        try FileManager.default.copyItem(at: Bundle.main.bundleURL, to: target)
    } catch {
        appLog("could not copy the app to Applications: \(error.localizedDescription)")
        return false
    }
    let conf = NSWorkspace.OpenConfiguration()
    conf.createsNewApplicationInstance = true
    NSWorkspace.shared.openApplication(at: target, configuration: conf) { _, _ in
        DispatchQueue.main.async { exit(0) }
    }
    return true
}

// MARK: - Earlier installs

/// takeOverLaunchAgent removes the service an earlier install.sh set up; its
/// settings and data are in the same folder and stay.
func takeOverLaunchAgent() {
    let label = "app.moviestracker"
    let plist = home.appendingPathComponent("Library/LaunchAgents/\(label).plist")
    guard FileManager.default.fileExists(atPath: plist.path) else { return }
    let target = "gui/\(getuid())/\(label)"
    run("/bin/launchctl", "bootout", target)
    for _ in 0..<20 where run("/bin/launchctl", "print", target) == 0 { sleep(1) }
    try? FileManager.default.removeItem(at: plist)
    try? FileManager.default.removeItem(at: dataDir.appendingPathComponent("bin"))
    appLog("replaced the launchd service of an earlier install; settings and data kept")
}

@discardableResult
func run(_ tool: String, _ args: String...) -> Int32 {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: tool)
    p.arguments = args
    p.standardOutput = FileHandle.nullDevice
    p.standardError = FileHandle.nullDevice
    guard (try? p.run()) != nil else { return -1 }
    p.waitUntilExit()
    return p.terminationStatus
}

// MARK: - App

final class App: NSObject, NSApplicationDelegate, NSMenuDelegate {
    let server = Server()
    var item: NSStatusItem!
    var gstAtStart: String?
    var gstNow: String?
    var openedOnce = UserDefaults.standard.bool(forKey: "openedOnce")

    func applicationDidFinishLaunching(_ note: Notification) {
        // The log needs its folder, which an uninstall may have deleted.
        try? FileManager.default.createDirectory(at: dataDir, withIntermediateDirectories: true,
                                                 attributes: [.posixPermissions: 0o700])
        appLog("Moviestracker \(appVersion) starting on macOS \(ProcessInfo.processInfo.operatingSystemVersionString) from \(Bundle.main.bundlePath)")
        let me = Bundle.main.bundleIdentifier ?? "app.moviestracker"
        if NSRunningApplication.runningApplications(withBundleIdentifier: me).count > 1 {
            appLog("another copy of the app runs: opening it instead")
            NSWorkspace.shared.open(localURL)
            NSApp.terminate(nil)
            return
        }

        if runningFromDownload {
            appLog("running from a disk image or download folder: asking to move to Applications")
            if !offerMove() { return }
        }

        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        let icon = NSImage(named: "MenuIcon")
        icon?.isTemplate = true
        item.button?.image = icon
        item.button?.toolTip = "Moviestracker"
        let menu = NSMenu()
        menu.delegate = self
        item.menu = menu

        server.onChange = { [weak self] in self?.stateChanged() }
        Timer.scheduledTimer(withTimeInterval: 3600, repeats: true) { _ in rotateLog() }
        if !UserDefaults.standard.bool(forKey: "loginItemOffered") && !runningFromDownload {
            UserDefaults.standard.set(true, forKey: "loginItemOffered")
            // Off the main thread: macOS may take its time over a new login item.
            DispatchQueue.global().async {
                appLog("turning on Start at Login")
                do {
                    try SMAppService.mainApp.register()
                    appLog("Start at Login is on")
                } catch {
                    appLog("Start at Login could not be turned on: \(error.localizedDescription)")
                }
            }
        }
        DispatchQueue.global().async {
            appLog("checking for an earlier install and GStreamer")
            takeOverLaunchAgent()
            let gst = gstreamerVersion()
            DispatchQueue.main.async {
                self.gstAtStart = gst
                self.gstNow = gst
                appLog("starting the server")
                self.server.start()
            }
        }
        stateChanged()
    }

    /// offerMove asks to copy the app to Applications when it runs from the
    /// DMG or a download; false means the app is quitting or moving.
    func offerMove() -> Bool {
        let a = NSAlert()
        a.messageText = "Move Moviestracker to Applications"
        a.informativeText = "Moviestracker is running from the disk image or a download folder. Copy it to Applications so it can start at login and keep working after the disk image is ejected."
        a.addButton(withTitle: "Move to Applications")
        a.addButton(withTitle: "Quit")
        a.addButton(withTitle: "Run From Here")
        NSApp.activate(ignoringOtherApps: true)
        switch a.runModal() {
        case .alertFirstButtonReturn:
            if moveToApplications() { return false }
            alert("Could not copy Moviestracker", "Drag Moviestracker from the disk image to the Applications folder in Finder, then open it from there.")
            NSApp.terminate(nil)
            return false
        case .alertSecondButtonReturn:
            NSApp.terminate(nil)
            return false
        default:
            return true
        }
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        server.stop { NSApp.reply(toApplicationShouldTerminate: true) }
        return .terminateLater
    }

    func stateChanged() {
        item.button?.appearsDisabled = server.state != .running && server.state != .otherInstance
        if server.state == .running && !openedOnce {
            // The first time: open the setup page.
            openedOnce = true
            UserDefaults.standard.set(true, forKey: "openedOnce")
            NSWorkspace.shared.open(localURL)
        }
    }

    // The menu is built each time it opens, so it always shows the current state.
    func menuNeedsUpdate(_ menu: NSMenu) {
        menu.removeAllItems()
        let status: String
        switch server.state {
        case .starting: status = "Moviestracker is starting…"
        case .running: status = "Moviestracker is running"
        case .otherInstance: status = "Another Moviestracker is using port \(port)"
        case .stopped(let why): status = "Moviestracker \(why)"
        }
        menu.addItem(disabled("Moviestracker \(appVersion)"))
        menu.addItem(disabled(status))
        menu.addItem(.separator())
        menu.addItem(action("Open Moviestracker", #selector(openHome), key: "o"))
        menu.addItem(action("Dashboard", #selector(openDashboard)))
        if let ip = lanAddress() {
            let lan = action("On a TV or phone: http://\(ip):\(port)", #selector(copyLAN))
            lan.representedObject = "http://\(ip):\(port)"
            lan.toolTip = "Click to copy the address"
            menu.addItem(lan)
        }
        menu.addItem(.separator())
        addGStreamer(to: menu)
        switch SMAppService.mainApp.status {
        case .requiresApproval:
            menu.addItem(action("Start at Login: allow it in System Settings…", #selector(openLoginItems)))
        default:
            let login = action("Start at Login", #selector(toggleLogin))
            login.state = SMAppService.mainApp.status == .enabled ? .on : .off
            menu.addItem(login)
        }
        menu.addItem(action("Show Logs", #selector(showLogs)))
        menu.addItem(action("Restart", #selector(restart)))
        menu.addItem(.separator())
        menu.addItem(action("Uninstall Moviestracker…", #selector(uninstall)))
        menu.addItem(action("Quit Moviestracker", #selector(quit), key: "q"))

        // Refresh GStreamer for the next time the menu opens.
        DispatchQueue.global().async {
            let v = gstreamerVersion()
            DispatchQueue.main.async { self.gstNow = v }
        }
    }

    /// addGStreamer says whether MKV files play in the browser; setting
    /// GStreamer up happens on the Sources page, which shows its download.
    func addGStreamer(to menu: NSMenu) {
        if let v = gstNow, newEnough(v) {
            if gstAtStart == nil || !newEnough(gstAtStart!) {
                menu.addItem(action("GStreamer \(v) installed: Restart to play MKV in the browser", #selector(restart)))
            } else {
                menu.addItem(disabled("GStreamer \(v): MKV plays in the browser"))
            }
        } else {
            menu.addItem(action("Set Up Browser Playback (GStreamer)…", #selector(openGStreamerSetup)))
        }
    }

    func action(_ title: String, _ sel: Selector, key: String = "") -> NSMenuItem {
        let i = NSMenuItem(title: title, action: sel, keyEquivalent: key)
        i.target = self
        return i
    }

    func disabled(_ title: String) -> NSMenuItem {
        let i = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        i.isEnabled = false
        return i
    }

    @objc func openHome() { NSWorkspace.shared.open(localURL) }
    @objc func openDashboard() { NSWorkspace.shared.open(localURL.appendingPathComponent("dashboard")) }
    @objc func showLogs() { NSWorkspace.shared.open(logURL) }
    @objc func restart() { server.restart(); gstAtStart = gstNow }
    @objc func quit() { NSApp.terminate(nil) }

    @objc func copyLAN(_ sender: NSMenuItem) {
        guard let url = sender.representedObject as? String else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(url, forType: .string)
    }

    @objc func openLoginItems() { SMAppService.openSystemSettingsLoginItems() }

    @objc func toggleLogin() {
        let svc = SMAppService.mainApp
        do {
            if svc.status == .enabled { try svc.unregister() } else { try svc.register() }
        } catch {
            alert("Start at Login could not be changed", error.localizedDescription)
        }
    }

    @objc func openGStreamerSetup() {
        NSWorkspace.shared.open(URL(string: "http://localhost:\(port)/settings/sources#gst-setup")!)
    }

    @objc func uninstall() {
        let a = NSAlert()
        a.messageText = "Uninstall Moviestracker?"
        a.informativeText = "Moviestracker and TorrServer stop and are removed: the app moves to the Trash, and TorrServer's torrent list and settings and the GStreamer Moviestracker downloaded are deleted."
        a.showsSuppressionButton = true
        a.suppressionButton?.title = "Also delete Moviestracker's accounts and settings"
        a.suppressionButton?.state = .off
        a.addButton(withTitle: "Uninstall")
        a.addButton(withTitle: "Cancel")
        a.alertStyle = .warning
        NSApp.activate(ignoringOtherApps: true)
        guard a.runModal() == .alertFirstButtonReturn else { return }
        let purge = a.suppressionButton?.state == .on
        server.stop {
            try? SMAppService.mainApp.unregister()
            self.removeTorrServerAndGStreamer()
            if purge {
                try? FileManager.default.removeItem(at: dataDir)
            }
            if let id = Bundle.main.bundleIdentifier {
                UserDefaults.standard.removePersistentDomain(forName: id)
                for dir in ["Library/Caches", "Library/HTTPStorages", "Library/Saved Application State"] {
                    let base = home.appendingPathComponent(dir)
                    for name in [id, id + ".savedState"] {
                        try? FileManager.default.removeItem(at: base.appendingPathComponent(name))
                    }
                }
            }
            NSWorkspace.shared.recycle([Bundle.main.bundleURL]) { _, _ in
                DispatchQueue.main.async { exit(0) }
            }
        }
    }

    /// removeTorrServerAndGStreamer deletes what goes with the app: TorrServer
    /// (a copy still running too) with its data, and the GStreamer the server
    /// downloaded with its plugin cache.
    func removeTorrServerAndGStreamer() {
        let engine = dataDir.appendingPathComponent("engine")
        run("/usr/bin/pkill", "-f", "--", "--path \(engine.path)")
        try? FileManager.default.removeItem(at: engine)
        try? FileManager.default.removeItem(at: gstRoot)
        let tmp = URL(fileURLWithPath: NSTemporaryDirectory())
        for name in (try? FileManager.default.contentsOfDirectory(atPath: tmp.path)) ?? []
        where name.hasPrefix("torrserver-gstreamer-registry") {
            try? FileManager.default.removeItem(at: tmp.appendingPathComponent(name))
        }
    }

    func alert(_ title: String, _ text: String) {
        let a = NSAlert()
        a.messageText = title
        a.informativeText = text
        NSApp.activate(ignoringOtherApps: true)
        a.runModal()
    }
}

let app = NSApplication.shared
let delegate = App()
app.delegate = delegate
app.setActivationPolicy(.accessory)
app.run()
