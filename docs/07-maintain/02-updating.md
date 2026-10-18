---
title: "Update photon-server"
summary: "Move photon-server to a new version, and go back to the old one if you need to."
---

A new version of photon-server may change how its database is laid out. These changes are called **migrations**, and `photon-server migrate` applies them before the new version starts. The standard Docker setup, the Flatpak and the Mac app do this for you.

When a newer release is out, Settings › Server › Dashboard says so under **Needs attention**, with a link to its notes. The server asks GitHub for it a few times a day. To stop it asking, set **Look for new versions** to **Off** under Settings › Server › General.

## Before you update

1. Note the version you run. Settings › Server › Dashboard shows it under the server's name.
2. Back up: open Settings › Server › Backups, choose **Back up now**, then **Download** the new backup. A migration can't be undone, so this backup is your way back.
3. Read the notes for the new release on [the releases page](https://github.com/olivertgwalton/photon-server/releases).

![Settings › Server › Dashboard, with the version shown under the server's name](../images/maintain-version.webp)

## Update with Docker

In the folder with your Compose file, run:

```sh
docker compose pull && docker compose up -d
```

This downloads the new version, migrates the database, and starts the server. FFmpeg and the other tools the server uses come with it.

Which version you get is set by `PHOTON_IMAGE` in `.env`:

| Value | What you get |
|---|---|
| `…:main` | The default: the newest build, updated often. |
| `…:latest` | The newest release. |
| A version, such as `…:1.4.0` | That release only. Updating changes nothing until you change it. |

For example, to follow releases:

```sh
PHOTON_IMAGE=ghcr.io/olivertgwalton/photon-server:latest
```

> **Note:** The old server lets the films it is sending play to the end before it stops, for up to two hours. To update without waiting, run `docker compose kill -s SIGTERM server` while it waits.

## Update a native install

- **Flatpak.** Download the new `.flatpak` from the release, and install it as you did the first one:

  ```sh
  flatpak install --user ./photon-server_<version>_linux_amd64.flatpak
  ```

  This replaces the old version and keeps your database. Stop Photon and open it again: it migrates the database as it starts. `flatpak update` doesn't find new versions, as the bundle doesn't come from a Flatpak repository.
- **Mac app.** Choose **Check for Updates…** in Photon's menu, then **Install Update**. Photon also looks once a day and asks you when a newer version is out. It stops the server, puts the new version in place, and opens it, which migrates the database. A version with no **Check for Updates…** is updated once by hand: quit Photon, drag the new one from the release's disk image to Applications, and open it.
- **Archive.** Stop the server. Unpack the new archive over the old one. Then, with `PHOTON_DATABASE_URL` set as before, run:

  ```sh
  photon-server/bin/photon-server migrate
  photon-server/bin/photon-server
  ```

  On Windows, stop the service, replace the folder, run `bin\photon-server.exe migrate`, and start the service again.

If you forget to migrate, the server won't start, and its log says so:

```text
schema version mismatch: database is at 31, this binary needs 32 (run photon-server migrate)
```

## Several servers

The database is shared, so update every node together:

1. Back up, as above.
2. Stop every node.
3. Run `photon-server migrate` once, from any one of them.
4. Start every node on the new version.

A node still on the old version won't start against the updated database.

## Go back to an earlier version

You can't migrate backwards, and an earlier version won't start on a database a later one has updated. To go back:

1. Stop every server.
2. Using the earlier version, restore the backup you made before updating, from the command line. See [restore from the command line](01-backups.md#restore-from-the-command-line).
3. Start the earlier version.

Anything that changed since that backup is lost.
