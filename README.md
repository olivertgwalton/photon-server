# photon-server

A media server for films and television, written in Go.

## Develop

```sh
go test ./...
go run ./cmd/photon-server
```

The server listens on `:8640` (`PHOTON_LISTEN`) and names itself after the host (`PHOTON_NAME`).

## Contributing

See [CLAUDE.md](CLAUDE.md): one concern per branch, each PR a short series of small commits,
squash-merged into `main` once CI passes.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
