import ServiceManagement
import SwiftUI

@main
struct PhotonApp: App {
    @NSApplicationDelegateAdaptor private var delegate: AppDelegate

    var body: some Scene {
        MenuBarExtra("Photon", systemImage: "play.rectangle.fill") {
            PhotonMenu(server: delegate.server)
        }
    }
}

/// AppDelegate stops the server however the app is quit: from its menu, at logout, or by another
/// app asking.
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let server = Server()

    func applicationWillTerminate(_: Notification) {
        server.stop()
    }
}

struct PhotonMenu: View {
    @ObservedObject var server: Server
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
        Divider()
        Button("Quit Photon") { NSApp.terminate(nil) }
            .keyboardShortcut("q")
    }
}
