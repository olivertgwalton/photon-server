---
title: "Update photon-server"
summary: "Move photon-server to a new version, migrate its database, and go back to the old one if you need to."
---

A new version of photon-server comes with any changes to its database as migrations, which `photon-server migrate` applies before the new server starts. This page covers updating each kind of install, what the migration does, several servers, and going back.

## Before you update

1. Note the version you run: Settings › Server › Dashboard shows it under the server's name, and so does the server to anyone:

   ```sh
   curl https://photon.example/api/v1/server
   ```

2. Back up the database: Settings › Server › Backups › **Back up now**, then **Download** the new dump to keep it off the machine. A migration cannot be undone, so this dump is the way back (see [going back](#go-back-to-an-earlier-version)).
3. Read the release's notes at [the releases page](https://github.com/olivertgwalton/photon-server/releases).

## Update with Docker

The compose file in [install with Docker](../01-get-started/02-install-docker.md) runs `migrate` as a service of its own, which the server waits on, so pulling and recreating does both:

```sh
docker compose pull && docker compose up -d
```

Which image it pulls is `PHOTON_IMAGE` in `.env`:

| Tag | What it is |
|---|---|
| `main` | The compose file's default: the newest build of the main branch. |
| `latest` | The newest release. |
| A version, such as `1.4.0` | That release, and only it, so `pull` changes nothing until you change the tag. |

```sh
PHOTON_IMAGE=ghcr.io/olivertgwalton/photon-server:latest
```

The image carries the FFmpeg, ffprobe, yt-dlp and PostgreSQL client tools it runs, so they are updated with it.

Recreating the server stops the old one as any stop does: it takes no new streams and plays those it has to their end, up to two hours, which is why the compose file gives it `stop_grace_period: 2h`. To update without waiting, stop it again while it drains, and it stops at once:

```sh
docker compose kill -s SIGTERM server
```

## Update a native install

- **Flatpak.** `flatpak update io.github.olivertgwalton.Photon`. Photon migrates the database every time it starts.
- **Mac app.** Quit Photon, drag the new one from the release's DMG to Applications, and open it. It migrates the database every time it starts.
- **Archive.** Stop the server, unpack the new archive in place of the old one, keeping its layout, then migrate and start it:

  ```sh
  photon-server/bin/photon-server migrate
  photon-server/bin/photon-server
  ```

  with `PHOTON_DATABASE_URL` set as it was. On Windows, stop the service, replace the folder, run `bin\photon-server.exe migrate` with the same environment, and start the service again.

## What the migration does

`photon-server migrate` applies, in order, each of the version's migrations the database has not had, logging a `migrated` line for each, and exits. A database already up to date is left as it is. It needs only `PHOTON_DATABASE_URL`.

The server itself never migrates: it refuses to start against a database at any other schema version than its own, and says so:

```text
schema version mismatch: database is at 31, this binary needs 32 (run photon-server migrate)
```

That is deliberate: a migration is run once, on purpose, rather than by whichever server starts first.

## Several servers

The database is shared, so it is migrated once, and every node must then run the version it was migrated to: a node of the old version cannot start again until it is updated, and a node of a newer version cannot start until the database is migrated. Update every node together:

1. Back up, as above.
2. Stop every node. Each plays its streams to their end first, up to two hours; stop it again to stop it at once.
3. Migrate once, from any one of them.
4. Start each node on the new version.

## Go back to an earlier version

There are no migrations backwards, and an earlier version refuses a database a later one migrated. To go back:

1. Stop every server.
2. With the earlier version, restore the dump you made before updating, from the command line (see [restore from the command line](01-backups.md#restore-from-the-command-line)):

   ```sh
   photon-server restore photon-20261016T030000Z.dump
   ```

3. Start the earlier version.

Whatever changed since that dump is lost. A dump made by the later version is refused by the earlier one, as it cannot know the later schema.
