---
title: "Install natively"
summary: "Run photon-server without Docker: the Mac app, the Windows app, the Linux desktop app, or a download for Linux, macOS or Windows."
---

If you would rather not use [Docker](02-install-docker.md), there are four other ways to run
photon-server. All of them are on the
[releases page](https://github.com/olivertgwalton/photon-server/releases). By the end, the server
is running and ready to [set up](04-first-run.md).

| Choose | If you have | It runs the database for you |
| --- | --- | --- |
| The Mac app (the `.dmg`) | A Mac with Apple silicon, macOS 13 or later | Yes |
| The Windows app (the `_setup.exe`) | A Windows 10 or 11 PC with an Intel or AMD processor | Yes |
| The Linux desktop app (the `.flatpak`) | A Linux desktop | Yes |
| A download (the `.tar.gz` or `.zip`) | Linux, macOS or Windows, and you are comfortable running a database yourself | No |

Pick the file that matches your machine: `arm64` for Apple silicon and other ARM machines, `amd64`
for Intel and AMD.

> **Note:** The Mac app, the Windows app and the Linux desktop app run only while you are logged in. For a server
> that runs from the moment the machine starts, use Docker, or a download set up as a service.

## macOS: the Mac app

1. Open the `.dmg` file.
2. Drag **Photon** into Applications.
3. Open Photon from Applications.

Photon appears in the menu bar at the top of the screen, not in the Dock. The first time, it
prepares its database, which takes a moment. When the menu says **Running**, choose **Open Photon**
and your browser opens the **Set up** page.


| Menu item | What it does |
| --- | --- |
| Running, Starting… or Stopped | What the server is doing. If it stopped, the reason is shown. |
| Open Photon | Opens the web app in your browser. |
| Start at Login | Opens Photon whenever you log in to the Mac. Off at first. |
| Show Logs | Opens the folder of log files, useful when something goes wrong. |
| Check for Updates… | Looks for a newer version, and installs it if you choose to. Photon also looks once a day. |
| Quit Photon | Stops the server. |

The Mac app uses the Mac's own video hardware to convert video.

## Windows: the Windows app

1. Open the `_setup.exe` file. It installs Photon for your Windows account, so it doesn't ask for
   an administrator.
2. The download isn't signed, so Windows may say it protected your PC. Choose **More info**, then
   **Run anyway**.

When it finishes, Photon starts and appears in the notification area at the right of the taskbar.
It may be hidden under the arrow there. The first time, it prepares its database, which takes a
moment. Windows then asks whether photon-server may use the network: allow it on private
networks, so your other devices can reach it. When Photon's menu says **Running**, choose
**Open Photon** and your browser opens the **Set up** page.

Click Photon's icon for its menu, which has the same items as the Mac app's, with
**Start at Sign-in** for **Start at Login**. Opening Photon from the Start menu again opens the web
app.

It reads libraries on your drives and on network shares you have mapped. It uses an NVIDIA, Intel
or AMD graphics card to convert video where it can. To remove it, uninstall **Photon** in
Settings › Apps. That leaves its database, in case you install it again.

## Linux: the Flatpak

Flatpak is a common way to install desktop apps on Linux. Install the file you downloaded:

```sh
flatpak install --user ./photon-server_<version>_linux_amd64.flatpak
```

This installs it for your user, and downloads the shared parts it needs from Flathub. Then open
**Photon** from your applications, or type:

```sh
flatpak run io.github.olivertgwalton.Photon
```

Each time it opens, it starts the server and opens the web app in your browser.

It can read libraries anywhere on the machine, such as your home folder, `/mnt`, `/media` or
`/srv`, and never changes them. It uses your graphics card to convert video where it can.

If you started it from a terminal, press Ctrl+C there to stop it. Otherwise, type:

```sh
pkill -TERM -f '/app/bin/photon$'
```

## The archives

A download is the server on its own. You install and run the database (PostgreSQL 18) and the
small store beside it (Valkey 9) yourself, as [Requirements](01-requirements.md) describes. Unpack
the download anywhere, but keep its folders as they are: the server finds its web app and tools
by where they sit.

### Prepare the database

1. Install PostgreSQL 18, and make it a user and a database for photon-server:

   ```sh
   createuser --pwprompt photon
   createdb --owner photon photon
   ```

   The first command makes a user called `photon` and asks you for its password. The second makes
   a database of the same name, owned by that user.

2. Make sure PostgreSQL's tools (`pg_dump`, `pg_restore` and `psql`) can be found on the machine's
   path. Without them, the server runs but cannot make or restore
   [backups](../07-maintain/01-backups.md).
3. Install Valkey 9. If your system only offers an older version, get it from
   [valkey.io](https://valkey.io/download/).

### Start it on Linux or macOS

```sh
tar -xzf photon-server_<version>_linux_amd64.tar.gz
export PHOTON_DATABASE_URL=postgres://photon:<password>@localhost/photon
export PHOTON_VALKEY_URL=valkey://localhost:6379
photon-server/bin/photon-server migrate
photon-server/bin/photon-server
```

In order, these lines:

1. Unpack the download.
2. Tell the server where the database is, with the password you chose above.
3. Tell the server where Valkey is.
4. Prepare the database for this version of the server.
5. Start the server.

> **Important:** Run the `migrate` line again after every update, before you start the server. It
> does not do this on its own.

On macOS, Homebrew's `postgresql@18` and `valkey` work. The download is not signed by Apple, so
before the first run, tell macOS to allow it:

```sh
xattr -dr com.apple.quarantine photon-server
```

To keep the server running from boot, run it under your system's service manager, such as systemd.
See **Good to know** below for two things to set.

### Start it on Windows

Valkey does not run on Windows by itself, so on Windows it is usually easier to use
[the Windows app](#windows-the-windows-app), which carries its own. The download suits you if you
already run PostgreSQL on Windows and can run Valkey in WSL (Windows' built-in Linux) or Docker.

1. Unpack the download.
2. Set `PHOTON_DATABASE_URL` and `PHOTON_VALKEY_URL` as environment variables, as in the Linux
   example above.
3. Run `bin\photon-server.exe migrate` to prepare the database.
4. Run `bin\photon-server.exe` to start the server.

PostgreSQL's Windows installer does not make its tools findable. Add its `bin` folder, such as
`C:\Program Files\PostgreSQL\18\bin`, to your path, or the server cannot make backups.

The server uses an NVIDIA, Intel or AMD graphics card to convert video where it can.

### Run it as a Windows service

A service starts with Windows, before anyone logs in. Open PowerShell as an administrator. With the
download unpacked in `C:\photon-server` and the database prepared as above, type:

```powershell
sc.exe create photon-server binPath= "C:\photon-server\bin\photon-server.exe" start= auto
reg add HKLM\SYSTEM\CurrentControlSet\Services\photon-server /v Environment /t REG_MULTI_SZ /f `
  /d "PHOTON_DATABASE_URL=postgres://photon:<password>@localhost/photon\0PHOTON_VALKEY_URL=valkey://localhost:6379"
sc.exe failure photon-server reset= 0 actions= restart/5000
sc.exe failureflag photon-server 1
sc.exe start photon-server
```

In order, these make the service and start it with Windows, tell it where the database and Valkey
are, have Windows restart it if it stops unexpectedly, and start it now.

The service runs as Windows' own system account, so that account must be able to read your
libraries, and PostgreSQL's tools must be on the system's path, not only yours. Its log file,
`photon-server.log`, is in `C:\Windows\System32\config\systemprofile\AppData\Local\photon-server`.
That file is never trimmed, so check its size now and then.

## Good to know

- **Stopping is gentle.** On a download or the Windows service, stopping the server lets anyone
  watching finish their film or episode, for up to two hours. Stop it a second time to stop it at
  once. The Mac app, the Windows app, and the Linux desktop app stopped with the command above,
  stop at once.
- **Restarts after a restore.** When you restore a backup, the server stops itself so it can be
  started again on the restored database. Docker, the apps and the Windows service above do this
  for you. If you use systemd or another service manager, have it restart the server whenever it
  stops with an error, and give it up to two hours to stop.
- **Where the files are.** The Mac app keeps its database in
  `~/Library/Application Support/Photon` and its logs in `~/Library/Logs/Photon`. The Windows app
  keeps both in `%LOCALAPPDATA%\Photon`, and itself in `%LOCALAPPDATA%\Programs\Photon`. The Linux
  desktop app keeps its database in `~/.var/app/io.github.olivertgwalton.Photon/data/photon`.
- Every setting and command the server understands is in
  [Configuration](../08-reference/01-configuration.md).
