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
```

Then open `http://<server>:8640` from the server's local network to set it up, as Jellyfin's wizard
does: its first admin (`POST /api/v1/setup`), its name and metadata language, its libraries, and
how it is reached from away. As Plex's claiming is, setting up is open only while the server has
no profile, and only to a client on its local networks that reaches it directly: a new server
trusts no reverse proxy, so it is set up at its own address, and the proxy trusted after under
Settings, Server, Network, with the address it is reached at from outside.

A forgotten password is reset from the login page, as Jellyfin resets one: asked for from the
server's local network, its code is written to the server's log (`docker compose logs server`)
for the operator to hand on, and lasts 30 minutes (`POST /api/v1/auth/password-resets`).

### Backups

Every three days the server dumps its database with `pg_dump` into its user's config folder
(`~/.config/photon-server/backups`; the image's `/var/lib/photon-server/backups`), keeping the
newest three. Settings, Server, Backups lists them, downloads one and backs up now
(`GET /api/v1/admin/backups`). With several servers, a dump is kept by whichever made it, as it ran
the scheduled tasks; each lists its own.

Restore, beside a dump there, restores it as Jellyfin does, by restarting
(`POST /api/v1/admin/backups/{name}/restore`). A dump the server answering does not keep, or one
made by a newer server, is refused. Otherwise every server stops, ending every stream, and clients
are told the server will be back; the one keeping the dump waits, two minutes at most, for the
others to let go of the database, restores it, and stops too. Each exits with code 75
(EX_TEMPFAIL), for its restart policy to start it again: the deploy folder's
`restart: unless-stopped`, Docker's `on-failure` and systemd's `Restart=on-failure` all do; a
server with none is started again by hand. A server starting meanwhile waits, its `/readyz`
answering 503, until the restore is done, or until the server restoring has said nothing for 30
seconds, as one that died would. A server that serves HTTPS answers plain HTTP while it waits.
Settings, Server, Backups then says how the restore ended. Devices signed in since the dump was
made are signed out by it.

Where a server cannot start, a dump is restored from the command line, with every server stopped:

```sh
docker compose stop server    # every node; stopping one twice stops it without draining
docker compose run --rm server restore /var/lib/photon-server/backups/photon-20261008T120000Z.dump
docker compose start server
```

Either way, the restore refuses while anything is connected to the database, naming each connection, and
refuses a dump made by a newer server than itself. It replaces the database with the dump's in
one transaction, so a restore that fails changes nothing; migrates an older dump to this version;
and clears the server's keys in Valkey, which may name playbacks, nodes and pairings the dump
never had. Cached artwork, previews and transcodes are left: each server's sweeps forget what the
database no longer has. It runs `pg_restore` and `psql`, as it backs up with `pg_dump`, each found
on the `PATH`, as a role that may drop and create the database's `public` schema: its owner.

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

Needs PostgreSQL 18, Valkey 9 and FFmpeg 8 or newer (`ffmpeg` and `ffprobe` on the `PATH`). Without libass in its FFmpeg, as Homebrew's lacks, a server
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
