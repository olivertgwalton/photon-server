---
title: "Back up and restore"
summary: "The database dumps photon-server makes by itself, how to keep a copy elsewhere, and how to restore one from the web app or the command line."
---

photon-server keeps everything it knows, the libraries and what it matched, profiles, watch history, playlists and settings, in its PostgreSQL database, and dumps that database by itself every three days. This page covers where those dumps are, what they leave out, and putting the server back as a dump has it.

## What the server backs up

The scheduled task **Back up the database** runs `pg_dump` every three days, on whichever node runs the scheduled tasks, and keeps the newest three dumps, removing older ones. A dump is written whole or not at all, in `pg_dump`'s custom format, and named by when it was made, such as `photon-20261016T030000Z.dump`. Each one raises a "Backups" entry in Settings › Server › Activity, and the `A backup is made` event for [webhooks](../06-integrations/05-webhooks.md).

The dump is the database alone. It does not hold:

- **Your media.** The libraries are read where they are and never written.
- **What is kept in storage**: artwork, avatars, uploaded pictures, theme tunes and previews, on each server's disk or in a bucket (see [storage](05-storage.md)). Artwork from the providers is fetched again and previews made again, but avatars and pictures you uploaded are only there; back up the cache folder's `artwork` or the bucket too if you want them.
- **Valkey**, which holds nothing durable: playbacks, nodes, pairings and counts.

### Where the dumps are

In the `photon-server/backups` folder of the server's config folder: `/var/lib/photon-server/backups` in the Docker image (the compose file keeps it in the `backups` volume), `~/.config/photon-server/backups` on Linux, `~/Library/Application Support/photon-server/backups` on a Mac, and `%LOCALAPPDATA%\photon-server\backups` on Windows. Settings › Server › Dashboard shows the folder this node uses as **Backups**, with the space free on it.

The server needs the PostgreSQL client tools, `pg_dump`, `pg_restore` and `psql`, on its `PATH`, no older than the PostgreSQL they reach; the Docker image, the Flatpak and the Mac app carry them. Without them its log warns, as it starts, that the database is neither backed up nor restored, the task fails, and a restore is refused.

## Back up now, and keep a copy elsewhere

Settings › Server › Backups lists the dumps this node keeps, newest first, with their size. **Back up now** runs the task at once.

```sh
curl -X POST https://photon.example/api/v1/admin/tasks/backup_database/run \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

The three dumps sit beside the database on the same machine, so keep a copy somewhere else: **Download** on each dump, or the API:

```sh
curl -s https://photon.example/api/v1/admin/backups -H "Authorization: Bearer $ADMIN_TOKEN"
curl -o photon-20261016T030000Z.dump \
  https://photon.example/api/v1/admin/backups/photon-20261016T030000Z.dump \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Or copy the folder, or the Docker volume, with whatever backs the machine up. A dump is a plain `pg_dump` file, so `pg_restore --list` reads it anywhere.

With several servers, each node keeps the dumps it made while it ran the scheduled tasks, so another node may keep others; the page names the node it lists.

## Restore from the web app

Restoring puts the database back as the dump has it: whatever changed since is lost. Every stream stops, and every server stops and starts again, which takes a minute or two.

1. Open Settings › Server › Backups on the node that keeps the dump.
2. Choose **Restore** beside it, then **Restore and restart**.

```sh
curl -X POST https://photon.example/api/v1/admin/backups/photon-20261016T030000Z.dump/restore \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

What can be refused is refused before anything changes: a dump this node does not keep (`404`), one made by a newer version of photon-server than this one, a second restore while one is under way, and a node without the PostgreSQL tools (each `409`). A dump made by an older version is restored and then migrated to this one.

Then:

1. Every node stops within a few seconds, ending its streams.
2. The node restoring waits, up to two minutes, for every other connection to the database to close. Anything else connected to it, such as a `psql` you left open, keeps it waiting, and past two minutes the restore is given up.
3. It drops the database's tables and restores the dump in one transaction, committed only once the whole dump is in: a restore that fails leaves the database as it was.
4. It migrates the database to its own version and clears the server's keys in Valkey, which may name playbacks, nodes and pairings the dump never had.
5. Every node starts again.

While it runs, a node started again waits before it opens the database, answering `/health` and the API with `503` and `not_ready`, and a browser with a page saying the server is restoring, which reloads itself. Should the node restoring stop part way, the others start again 30 seconds after it last said it was restoring.

The page then says how the last restore ended: "The last restore, of photon-….dump, succeeded", or failed, with why.

### What starts the servers again

A server stopped for a restore exits with status 75, to be started again by whatever runs it. Docker's `restart: unless-stopped` and `on-failure`, systemd's `Restart=on-failure`, the Mac app, the Flatpak and the Windows service from [installing natively](../01-get-started/03-install-natively.md) all do. Anything else, start the servers again by hand.

Cached artwork and previews are left as they were; each server's sweeps forget what the restored database no longer has.

## Restore from the command line

Where a server cannot start, or on a new machine, restore with the server stopped. The command needs the same two addresses the server does, and refuses while any server, or anything else, is connected to the database.

```sh
export PHOTON_DATABASE_URL=postgres://photon:<password>@localhost/photon
export PHOTON_VALKEY_URL=valkey://localhost:6379
photon-server restore /path/to/photon-20261016T030000Z.dump
```

In Docker, stop the server and run the command in a container of its image, with the dump in its backups volume:

```sh
docker compose stop server
docker compose run --rm --no-deps server restore /var/lib/photon-server/backups/photon-20261016T030000Z.dump
docker compose up -d
```

It says what it did as it goes: the schema version the dump was made at, the migration to this version where it was older, and how many of the server's keys it cleared in Valkey.

### Move to a new machine

1. Install photon-server on the new machine with an empty database; there is no need to migrate it or set it up. In Docker, start only the database and Valkey: `docker compose up -d db valkey`.
2. Restore the dump from the command line, as above. In Docker, hand the container the dump, which must be readable by everyone, as the server runs as its own user:

   ```sh
   docker compose run --rm --no-deps -v "$PWD/photon-20261016T030000Z.dump:/tmp/photon.dump:ro" \
     server restore /tmp/photon.dump
   ```

3. Start the server. Mount the media at the same paths as before: a library is kept by its path.

Copy the old cache folder's `artwork` across too, or move to a bucket first, to keep avatars and uploaded pictures.
