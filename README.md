# photon-server

A media server for films and television, written in Go.

## Develop

Needs PostgreSQL 18, Valkey, and FFmpeg 9 or newer (`ffmpeg` and `ffprobe` on the `PATH`, or
`PHOTON_FFMPEG` and `PHOTON_FFPROBE`).

```sh
export PHOTON_DATABASE_URL=postgres://localhost/photon_dev
export PHOTON_VALKEY_URL=valkey://localhost:6379
go run ./cmd/photon-server migrate
go run ./cmd/photon-server
TEST_DATABASE_URL=postgres://localhost/postgres TEST_VALKEY_URL=valkey://localhost:6379 go test -tags integration ./...
```

The server listens on `:8640` (`PHOTON_LISTEN`) and names itself after the host (`PHOTON_NAME`).
Behind a reverse proxy, list the proxy's addresses in `PHOTON_TRUSTED_PROXIES` (for example
`172.16.0.0/12,127.0.0.1`); `X-Forwarded-For` is ignored from anyone else.

Titles are described by their file names, then any Kodi NFO beside them, and films and shows are
matched on TMDB in `PHOTON_METADATA_LANGUAGE` (default `en-US`, whose region picks certificates).
The server ships its own TMDB token; set `PHOTON_TMDB_TOKEN` to use yours. What a reader edits
or an NFO says is never replaced by a match.

The first admin is made on the command line, before anyone can sign in:

```sh
go run ./cmd/photon-server profile add -name Oliver -role admin
```
`GET /readyz` answers 204 while Postgres and Valkey are reachable and 503 otherwise.
Integration tests create and drop a database per test on the server `TEST_DATABASE_URL` names.

## Contributing

See [CLAUDE.md](CLAUDE.md): one concern per branch, each PR a short series of small commits,
squash-merged into `main` once CI passes.

## Metadata

This product uses the TMDB API but is not endorsed or certified by TMDB.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
