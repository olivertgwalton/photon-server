---
title: "Install natively"
summary: "Run photon-server without Docker: the Flatpak for Linux, the Mac app, or a release archive on Linux, macOS or Windows."
---

Each [release](https://github.com/olivertgwalton/photon-server/releases) carries three ways to run
photon-server outside a container. Pick the one that fits the machine, and you end with a server
answering on port 8640, ready to [set up](04-first-run.md).

| Download | For | Brings its own database |
| --- | --- | --- |
| `photon-server_<version>_linux_amd64.flatpak`, `…_linux_arm64.flatpak` | A Linux desktop | Yes |
| `photon-server_<version>_darwin_arm64.dmg` | A Mac with Apple silicon, macOS 13 or later | Yes |
| `photon-server_<version>_linux_amd64.tar.gz`, `…_linux_arm64.tar.gz` | A Linux server | No |
| `photon-server_<version>_darwin_amd64.tar.gz`, `…_darwin_arm64.tar.gz` | A Mac, Intel or Apple silicon | No |
| `photon-server_<version>_windows_amd64.zip` | Windows | No |

Every one carries the server, its web app, Jellyfin's web app, and the ffmpeg, ffprobe and yt-dlp
it runs (jellyfin-ffmpeg's portable build). The Flatpak and the Mac app also carry PostgreSQL 18
and Valkey 9 and start them for you; an archive uses a PostgreSQL and a Valkey you run, as
[Requirements](01-requirements.md) describes.

The Flatpak and the Mac app run while you are logged in. For a server that runs from boot, use
[Docker](02-install-docker.md), an archive under your service manager, or the Windows service
below.

## Linux: the Flatpak

Install the release's Flatpak for your machine, `amd64` or `arm64`. It fetches its runtime,
`org.freedesktop.Platform` 26.08, from Flathub:

```sh
flatpak install --user ./photon-server_<version>_linux_amd64.flatpak
```

Open Photon from your applications, or run:

```sh
flatpak run io.github.olivertgwalton.Photon
```

The first time, it makes its database in `~/.var/app/io.github.olivertgwalton.Photon/data/photon`.
Each time, it starts PostgreSQL, Valkey and the server, migrates the database and opens the web
app in your browser. PostgreSQL and Valkey listen only on sockets inside the app's own runtime
folder, so neither has a password or a port. Their logs are in that folder's `logs`:
`postgres.log` and `valkey.log`. The server's own log goes wherever `flatpak run`'s output does.

It reads libraries anywhere on the machine, a home folder, `/mnt`, `/media` or `/srv`, and writes
to none of them. It encodes on the GPU where the Flatpak runtime's drivers reach it.

Run from a terminal, Ctrl+C stops the server, Valkey and PostgreSQL together; otherwise, ask the
launcher inside the sandbox:

```sh
pkill -TERM -f '/app/bin/photon$'
```

Stopped this way, the server stops at once rather than playing its streams to their end. A
database made by another PostgreSQL major version is refused rather than started.

## macOS: the Mac app

On a Mac with Apple silicon, open the release's DMG and drag Photon to Applications. The app is
signed and notarised.

Photon runs in the menu bar. The first time it opens, it makes its database in
`~/Library/Application Support/Photon`. Each time, it starts PostgreSQL, Valkey and the server,
which listen only on sockets in that folder, and migrates the database. Its menu:

| Item | What it does |
| --- | --- |
| Running, Starting… or Stopped | What the server is doing, with the reason where it stopped. |
| Open Photon | Opens `http://localhost:8640` in your browser, once the server is running. |
| Start at Login | Opens Photon when you log in to the Mac. Off by default. |
| Show Logs | Shows `~/Library/Logs/Photon`: `photon-server.log`, `postgres.log` and `valkey.log`. |
| Quit Photon | Stops the server at once, then Valkey and PostgreSQL. |

The app encodes on VideoToolbox. The server keeps its cache in `~/Library/Caches/photon-server`
and its database dumps in `~/Library/Application Support/photon-server/backups`.

## The archives

An archive is laid out as an install is, and the server finds its web app and tools from where
its binary is, so keep the layout:

```text
photon-server/
  bin/          photon-server, ffmpeg, ffprobe, yt-dlp
  share/photon-server/web
  share/photon-server/jellyfin-web
```

The server looks for ffmpeg, ffprobe, yt-dlp, `pg_dump`, `pg_restore` and `psql` beside itself
first, then on the `PATH`.

### Prepare the database

Make PostgreSQL 18 a user and a database of their own:

```sh
createuser --pwprompt photon
createdb --owner photon photon
```

Put the PostgreSQL client tools, `pg_dump`, `pg_restore` and `psql`, on the server's `PATH`, no
older than the PostgreSQL they reach. Without them the server runs, but neither makes nor restores
[backups](../07-maintain/01-backups.md).

Install Valkey 9; where a distribution's own is older, take it from
[valkey.io](https://valkey.io/download/).

### Start it on Linux or macOS

Unpack the archive anywhere, set the two addresses, migrate, and start it:

```sh
tar -xzf photon-server_<version>_linux_amd64.tar.gz
export PHOTON_DATABASE_URL=postgres://photon:<password>@localhost/photon
export PHOTON_VALKEY_URL=valkey://localhost:6379
photon-server/bin/photon-server migrate
photon-server/bin/photon-server
```

Run `migrate` after every update, before the server starts: the server does not migrate on its
own. Every setting and subcommand is in [Configuration](../08-reference/01-configuration.md).

On macOS, Homebrew's `postgresql@18` and `valkey` serve. The archive's binaries are not signed, as
the DMG's are, so clear the download's quarantine before the first run:

```sh
xattr -dr com.apple.quarantine photon-server
```

Under a service manager such as systemd, have it restart the server when it exits with status 75
(see [Restarts after a restore](#restarts-after-a-restore)), and give a stop up to two hours: a
stopping server plays its streams to their end first, and a second stop signal stops it at once.

### Start it on Windows

Valkey has no Windows build, so on Windows the simplest server is
[Docker Desktop](02-install-docker.md) with the same Compose file. The archive is for those who
run PostgreSQL on Windows and Valkey in WSL or Docker.

Unpack the archive, set `PHOTON_DATABASE_URL` and `PHOTON_VALKEY_URL` as environment variables,
run `bin\photon-server.exe migrate`, then `bin\photon-server.exe`.

PostgreSQL's installer does not put its tools on the `PATH`. Add its `bin` folder, such as
`C:\Program Files\PostgreSQL\18\bin`, or the server neither backs up nor restores.

The server encodes on an NVIDIA GPU (NVENC), an Intel one (Quick Sync) or an AMD one (AMF),
whichever first encodes a test picture, or in software.

### Run it as a Windows service

The server runs under Windows' service manager as it is. From an administrator's PowerShell, with
the archive unpacked in `C:\photon-server` and the database migrated as above:

```powershell
sc.exe create photon-server binPath= "C:\photon-server\bin\photon-server.exe" start= auto
reg add HKLM\SYSTEM\CurrentControlSet\Services\photon-server /v Environment /t REG_MULTI_SZ /f `
  /d "PHOTON_DATABASE_URL=postgres://photon:<password>@localhost/photon\0PHOTON_VALKEY_URL=valkey://localhost:6379"
sc.exe failure photon-server reset= 0 actions= restart/5000
sc.exe failureflag photon-server 1
sc.exe start photon-server
```

It runs as Local System, which must be able to read
the libraries, and the PostgreSQL tools must be on the system's `PATH`, not only yours. It keeps
its cache, its database dumps and its log, `photon-server.log`, in
`C:\Windows\System32\config\systemprofile\AppData\Local\photon-server`; the log is never rotated.

Stopping the service lets the streams it serves play to their end, up to two hours; a shutdown of
the machine stops it at once. The failure actions start it again after a restore.

## Restarts after a restore

A server stopped to restore a backup exits with status 75 (`EX_TEMPFAIL`), to be started again on
the restored database. The Flatpak, the Mac app, the Windows service above and the Compose file
all do. Anything else that runs the server must restart it on that status, or on any failure.
