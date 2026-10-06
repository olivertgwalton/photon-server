# photon-server

A media server for films and television, written in Go.

## Tech stack

- **Server**: Go, PostgreSQL (GORM, pgx, goose migrations), Valkey
- **Media**: FFmpeg (jellyfin-ffmpeg in the image, with VAAPI, Quick Sync, NVIDIA and Vulkan)
- **Metadata**: TMDB, TheTVDB, Kodi NFO
- **Web**: SvelteKit (static build served by the Go server), Tailwind, bits-ui, Bun, Biome,
  Playwright

## Run

The image is `ghcr.io/olivertgwalton/photon-server` (amd64 and arm64). `deploy/` runs it with
PostgreSQL and Valkey:

```sh
cd deploy && cp .env.example .env    # set the passwords and MEDIA_DIR
docker compose up -d                 # add -f compose.intel.yml or -f compose.nvidia.yml for a GPU
docker compose exec server photon-server profile add -name Admin -role admin
docker compose exec server photon-server library add -name Films -kind movies /media/Films
```

Then open `http://<server>:8640` and log in.

## Develop

Needs PostgreSQL 18, Valkey 9 and FFmpeg 8 or newer (`ffmpeg` and `ffprobe` on the `PATH`, or
`PHOTON_FFMPEG` and `PHOTON_FFPROBE`).

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

## Contributing

See [CLAUDE.md](CLAUDE.md): one concern per branch, each PR a short series of small commits,
squash-merged into `main` once CI passes.

## Metadata

This product uses the TMDB API but is not endorsed or certified by TMDB.

Metadata provided by [TheTVDB](https://thetvdb.com). Please consider adding missing information or
subscribing.

## Licence

GPL-3.0. See [LICENSE](LICENSE).
