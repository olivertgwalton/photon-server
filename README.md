# photon-server

A media server for films and television, written in Go.

## Tech stack

- **Server**: Go, PostgreSQL (pgx, goose migrations), Valkey
- **Media**: FFmpeg (jellyfin-ffmpeg in the image, with VAAPI, Quick Sync, NVIDIA and Vulkan)
- **Metadata**: TMDB, TheTVDB, Kodi NFO
- **Web**: SvelteKit (static build served by the Go server), Tailwind, bits-ui, Bun, Biome,
  Playwright

## Deploy

The image is `ghcr.io/olivertgwalton/photon-server` (amd64 and arm64), run with PostgreSQL and
Valkey by the compose file in [docs/install.md](docs/install.md). Each release also has a Flatpak
for Linux and a macOS app in a DMG, each carrying its own database, and archives for Linux, macOS
and Windows, which the same guide installs.

Open `http://<server>:8640` from the server's local network to set it up: the first admin, the
server's name, its libraries and how it is reached from outside. Setup is open only while the
server has no profile, and only to a client on its local network reaching it directly, so set it
up at its own address and trust a reverse proxy afterwards under Settings, Server, Network.

A forgotten password is reset from the login page on the local network; the reset code is written
to the server's log (`docker compose logs server`) and lasts 30 minutes.

### Backups

The server dumps its database every three days into `/var/lib/photon-server/backups`, keeping the
newest three. Settings, Server, Backups lists, downloads, makes and restores them. To restore from
the command line when the server cannot start:

```sh
docker compose stop server
docker compose run --rm server restore /var/lib/photon-server/backups/photon-20261008T120000Z.dump
docker compose start server
```

### Several servers

Several servers can share one PostgreSQL and Valkey; see [docs/cluster.md](docs/cluster.md).
Plugins are described in [docs/plugins.md](docs/plugins.md).

## Develop

Needs Go, PostgreSQL 18, Valkey 9 and FFmpeg 8 or newer (`ffmpeg` and `ffprobe` on the `PATH`).

```sh
export PHOTON_DATABASE_URL=postgres://localhost/photon_dev
export PHOTON_VALKEY_URL=valkey://localhost:6379
go run ./cmd/photon-server migrate
go run ./cmd/photon-server
TEST_DATABASE_URL=postgres://localhost/postgres TEST_VALKEY_URL=valkey://localhost:6379 go test -tags integration ./...
```

The web app, from `web/`, with the server running:

```sh
bun install
bun run dev   # http://localhost:5173, /api passed on to PHOTON_API_URL (default http://localhost:8640)
bun run check && bun run lint && bun test src && bun run test:e2e
bun run api   # after changing a route: regenerates src/lib/api/schema.d.ts
```

## Pull requests

- One concern per branch, one branch per PR, branched off `main` as `feature/…`, `fix/…` or
  `chore/…`.
- A PR is a short series of small commits, each building and passing the tests on its own.
  Refactors and toolchain bumps go in their own commit, usually their own PR.
- Commits and PR titles are Conventional Commits: `type(scope): summary`, lower case, imperative,
  no full stop, under 72 characters.
- PRs are squash-merged into `main` once CI passes and the review against
  [the PR template](.github/pull_request_template.md) is done.
- `golangci-lint fmt` and `golangci-lint run` must both be silent.

The full rules are in [CLAUDE.md](CLAUDE.md).

## Metadata

This product uses the TMDB API but is not endorsed or certified by TMDB.

Metadata provided by [TheTVDB](https://thetvdb.com). Please consider adding missing information or
subscribing.

## Licence

GPL-3.0. See [LICENSE](LICENSE).
