# photon-server

A media server for films and television, written in Go.

## Tech stack

- **Server**: Go, PostgreSQL (pgx, goose migrations), Valkey
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

### Several servers

Several servers can serve one household together, sharing its PostgreSQL and Valkey; see
[docs/cluster.md](docs/cluster.md).

Each server keeps artwork (with avatars and theme tunes) and previews in its own cache folder
until an admin chooses an S3 bucket they all share (`PUT /api/v1/admin/storage`). The bucket is
checked before it is chosen. Without an access key, a server signs with its own AWS credentials:
`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`, the shared credentials file, or the instance's
role. What is kept is moved there: every server writes to both places, copies what it keeps, and
keeps everything in the new place once every copy is done. A server that is down during a move
keeps what is on its own disk there.

What is kept in a bucket goes to clients through the server, or, when an admin asks, clients are
sent to read pictures, sounds and previews from the bucket itself, at the address they reach it at.
A link lasts an hour. Anything that could run script, such as an SVG logo, still goes through the
server.

The server gzips its own JSON answers for a client that takes gzip, and serves the web app
precompressed; media, artwork and event streams go out as they are. A reverse proxy in front of it
need not compress again.

## Develop

Needs PostgreSQL 18, Valkey 9 and FFmpeg 8 or newer (`ffmpeg` and `ffprobe` on the `PATH`, or
`PHOTON_FFMPEG` and `PHOTON_FFPROBE`). Without libass in its FFmpeg, as Homebrew's lacks, a server
draws no styled subtitles (ASS) into video, and plays them only on clients that draw them.

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
