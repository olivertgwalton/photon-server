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
`GET /readyz` answers 204 while Postgres and Valkey are reachable and 503 otherwise.
Integration tests create and drop a database per test on the server `TEST_DATABASE_URL` names.

## Contributing

See [CLAUDE.md](CLAUDE.md): one concern per branch, each PR a short series of small commits,
squash-merged into `main` once CI passes.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
