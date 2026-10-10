import Foundation

/// Server runs photon-server from the app's Resources, as the release archive lays it out, beside
/// the PostgreSQL and Valkey the app carries: each listens on a socket in the app's own folder in
/// Application Support, which only this user can reach, so none needs a password or a port. It
/// makes the database the first time, migrates it, serves until stopped, and starts photon-server
/// again when it exits 75, as it does after a restore.
@MainActor
final class Server: ObservableObject {
    enum State: Equatable, CustomStringConvertible {
        case starting, running, stopped
        case failed(String)

        var description: String {
            switch self {
            case .starting: "Starting…"
            case .running: "Running"
            case .stopped: "Stopped"
            case .failed(let why): "Stopped: \(why)"
            }
        }
    }

    nonisolated static let address = URL(string: "http://localhost:8640")!
    nonisolated static let logs = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Logs/Photon")
    nonisolated static let log = logs.appendingPathComponent("photon-server.log")
    /// The PostgreSQL the app carries: a database another made is refused rather than started.
    nonisolated static let postgresMajor = "18"
    /// The status photon-server exits with when it is stopped for a restore, to be started again.
    nonisolated static let exitRestart: Int32 = 75

    nonisolated private static let resources = Bundle.main.resourceURL!
    nonisolated private static let postgres = resources.appendingPathComponent("postgres/bin")
    nonisolated private static let home = FileManager.default.homeDirectoryForCurrentUser
        .appendingPathComponent("Library/Application Support/Photon")
    nonisolated private static let data = home.appendingPathComponent("postgres")

    @Published private(set) var state = State.stopped
    private var valkey: Process?
    private var server: Process?
    private var stopping = false

    init() { start() }

    func start() {
        stopping = false
        state = .starting
        Task.detached {
            do {
                let valkey = try Server.startServices()
                await MainActor.run {
                    self.valkey = valkey
                    self.serve()
                }
            } catch {
                await MainActor.run { self.state = .failed("\(error)") }
            }
        }
    }

    /// stop stops photon-server at once, told twice to play no stream to its end, then Valkey and
    /// PostgreSQL.
    func stop() {
        stopping = true
        for p in [server, valkey].compactMap({ $0 }) where p.isRunning {
            p.terminate()
            if p == server { p.terminate() }
            p.waitUntilExit()
        }
        server = nil
        valkey = nil
        _ = try? Server.run(Server.postgres.appendingPathComponent("pg_ctl"),
                            ["-D", Server.data.path, "-m", "fast", "-w", "stop"], log: "postgres.log")
        state = .stopped
    }

    private func serve() {
        var env = ProcessInfo.processInfo.environment
        // pg_dump, pg_restore and psql back the database up and put it back.
        env["PATH"] = Server.postgres.path + ":" + (env["PATH"] ?? "/usr/bin:/bin")
        var db = URLComponents(string: "postgres://photon@/photon")!
        db.queryItems = [URLQueryItem(name: "host", value: Server.home.path)]
        env["PHOTON_DATABASE_URL"] = db.string!
        env["PHOTON_VALKEY_URL"] = URL(fileURLWithPath: Server.home.appendingPathComponent("valkey.sock").path)
            .absoluteString.replacingOccurrences(of: "file://", with: "unix://")
        launch(["migrate"], env: env) { [weak self] code in
            guard let self, !self.stopping else { return }
            guard code == 0 else {
                self.state = .failed("the database was not migrated (\(code)): see the log")
                return
            }
            self.state = .running
            self.launch([], env: env) { [weak self] code in
                guard let self, !self.stopping else { return }
                switch code {
                case Server.exitRestart: self.serve()
                case 0: self.state = .stopped
                default: self.state = .failed("photon-server exited \(code): see the log")
                }
            }
        }
    }

    private func launch(_ args: [String], env: [String: String], done: @escaping @MainActor (Int32) -> Void) {
        let p = Process()
        p.executableURL = Server.resources.appendingPathComponent("photon-server/bin/photon-server")
        p.arguments = args
        p.environment = env
        if let out = Server.logHandle("photon-server.log") {
            p.standardOutput = out
            p.standardError = out
        }
        p.terminationHandler = { p in
            let code = p.terminationStatus
            Task { @MainActor in done(code) }
        }
        do {
            try p.run()
            server = p
        } catch {
            state = .failed("photon-server did not start: \(error.localizedDescription)")
        }
    }

    struct Failure: Error, CustomStringConvertible {
        let description: String
    }

    /// startServices makes the database the first time, starts PostgreSQL and then Valkey, and
    /// answers Valkey once its socket is there.
    nonisolated private static func startServices() throws -> Process {
        let fm = FileManager.default
        try fm.createDirectory(at: home, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let version = data.appendingPathComponent("PG_VERSION")
        if !fm.fileExists(atPath: version.path) {
            try run(postgres.appendingPathComponent("initdb"),
                    ["-D", data.path, "-U", "photon", "--auth=trust", "-E", "UTF8", "--locale=C"], log: "postgres.log")
            let conf = data.appendingPathComponent("postgresql.conf")
            let h = try FileHandle(forWritingTo: conf)
            h.seekToEndOfFile()
            h.write(Data("listen_addresses = ''\nunix_socket_directories = '\(home.path)'\n".utf8))
            try h.close()
        }
        let made = try String(contentsOf: version, encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
        guard made == postgresMajor else {
            throw Failure(description: "the database was made by PostgreSQL \(made), not \(postgresMajor)")
        }
        try run(postgres.appendingPathComponent("pg_ctl"),
                ["-D", data.path, "-l", logs.appendingPathComponent("postgres.log").path, "-w", "start"], log: "postgres.log")
        let psql = postgres.appendingPathComponent("psql")
        let exists = try run(psql, ["-h", home.path, "-U", "photon", "-d", "postgres", "-Atc",
                                    "SELECT 1 FROM pg_database WHERE datname = 'photon'"], log: "postgres.log")
        if exists.isEmpty {
            try run(psql, ["-h", home.path, "-U", "photon", "-d", "postgres", "-c", "CREATE DATABASE photon"], log: "postgres.log")
        }

        let socket = home.appendingPathComponent("valkey.sock")
        try? fm.removeItem(at: socket)
        let valkey = Process()
        valkey.executableURL = resources.appendingPathComponent("valkey/bin/valkey-server")
        // As the compose file's: nothing durable lives here, so snapshots only keep live sessions
        // across a restart.
        valkey.arguments = [
            "--port", "0", "--unixsocket", socket.path, "--unixsocketperm", "700", "--dir", home.path,
            "--appendonly", "no", "--save", "300 1", "--maxmemory", "256mb", "--maxmemory-policy", "volatile-lru",
            "--logfile", logs.appendingPathComponent("valkey.log").path,
        ]
        try valkey.run()
        for _ in 0..<100 where !fm.fileExists(atPath: socket.path) {
            Thread.sleep(forTimeInterval: 0.1)
        }
        guard fm.fileExists(atPath: socket.path) else {
            valkey.terminate()
            throw Failure(description: "Valkey did not start: see valkey.log")
        }
        return valkey
    }

    /// run runs a tool to its end, its errors written to the log named, and answers what it wrote.
    @discardableResult
    nonisolated private static func run(_ tool: URL, _ args: [String], log: String) throws -> String {
        let p = Process()
        p.executableURL = tool
        p.arguments = args
        let out = Pipe()
        p.standardOutput = out
        p.standardError = logHandle(log) ?? FileHandle.nullDevice
        try p.run()
        let said = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        guard p.terminationStatus == 0 else {
            throw Failure(description: "\(tool.lastPathComponent) failed: see \(log)")
        }
        return String(decoding: said, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
    }

    nonisolated private static func logHandle(_ name: String) -> FileHandle? {
        let fm = FileManager.default
        let file = logs.appendingPathComponent(name)
        try? fm.createDirectory(at: logs, withIntermediateDirectories: true)
        if !fm.fileExists(atPath: file.path) {
            fm.createFile(atPath: file.path, contents: nil, attributes: [.posixPermissions: 0o600])
        }
        guard let h = try? FileHandle(forWritingTo: file) else { return nil }
        h.seekToEndOfFile()
        return h
    }
}
