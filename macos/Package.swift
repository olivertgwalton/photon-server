// swift-tools-version:6.0
import PackageDescription

// Photon is the menu-bar app the macOS DMG carries: it runs the server bundled beside it, and
// Sparkle updates it from the appcast each release publishes.
let package = Package(
    name: "Photon",
    platforms: [.macOS(.v13)],
    dependencies: [.package(url: "https://github.com/sparkle-project/Sparkle", exact: "2.10.0")],
    targets: [
        .executableTarget(
            name: "Photon",
            dependencies: [.product(name: "Sparkle", package: "Sparkle")],
            // Sparkle.framework is in the app's Frameworks, where build-app.sh puts it.
            linkerSettings: [.unsafeFlags(["-Xlinker", "-rpath", "-Xlinker", "@executable_path/../Frameworks"])]
        ),
    ]
)
