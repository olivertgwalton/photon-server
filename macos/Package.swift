// swift-tools-version:6.0
import PackageDescription

// Photon is the menu-bar app the macOS DMG carries: it runs the server bundled beside it.
let package = Package(
    name: "Photon",
    platforms: [.macOS(.v13)],
    targets: [.executableTarget(name: "Photon")]
)
