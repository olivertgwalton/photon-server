import ServiceManagement
import Sparkle
import SwiftUI

@main
struct PhotonApp: App {
    @NSApplicationDelegateAdaptor private var delegate: AppDelegate

    var body: some Scene {
        // The Template suffix makes NSImage draw it in the menu bar's own colour.
        MenuBarExtra("Photon", image: "MenuBarIconTemplate") {
            PhotonMenu(server: delegate.server, updater: delegate.updater.updater)
        }
    }
}

/// AppDelegate stops the server however the app is quit: from its menu, at logout, by another app
/// asking, or by Sparkle to install an update, which opens the new version once it is in place.
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let server = Server()
    let updater = SPUStandardUpdaterController(startingUpdater: true, updaterDelegate: nil, userDriverDelegate: nil)

    func applicationWillTerminate(_: Notification) {
        server.stop()
    }
}

struct PhotonMenu: View {
    @ObservedObject var server: Server
    let updater: SPUUpdater
    @State private var atLogin = SMAppService.mainApp.status == .enabled

    var body: some View {
        Text(server.state.description)
        Divider()
        Button("Open Photon") { NSWorkspace.shared.open(Server.address) }
            .disabled(server.state != .running)
        Toggle("Start at Login", isOn: $atLogin)
            .onChange(of: atLogin) { on in
                do {
                    try on ? SMAppService.mainApp.register() : SMAppService.mainApp.unregister()
                } catch {
                    atLogin = SMAppService.mainApp.status == .enabled
                }
            }
        Button("Show Logs") { NSWorkspace.shared.activateFileViewerSelecting([Server.log]) }
        Button("Check for Updates…") { updater.checkForUpdates() }
        Divider()
        Button("Quit Photon") { NSApp.terminate(nil) }
            .keyboardShortcut("q")
    }
}
