# photon-server

A media server for films and television, written in Go.

## Develop

Needs PostgreSQL 18.

```sh
export PHOTON_DATABASE_URL=postgres://localhost/photon_dev
go run ./cmd/photon-server migrate
go run ./cmd/photon-server
TEST_DATABASE_URL=postgres://localhost/postgres go test -tags integration ./...
```

The server listens on `:8640` (`PHOTON_LISTEN`) and names itself after the host (`PHOTON_NAME`).
Integration tests create and drop a database per test on the server `TEST_DATABASE_URL` names.

## Contributing

See [CLAUDE.md](CLAUDE.md): one concern per branch, each PR a short series of small commits,
squash-merged into `main` once CI passes.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
