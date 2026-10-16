---
title: "Keep libraries up to date"
summary: "How photon-server notices new, changed and removed files: watching folders, the scan schedule, scans you start, and what a scan does with files that move or go missing."
---

Once a library is added, the server keeps it in step with its folder. Three things start a scan:
the folder watch, as files change; the schedule, twice a day; and you, or a tool such as Radarr or
Sonarr, asking for one. This page covers each, what a scan reads and skips, and what happens to a
title whose file is renamed, moved or gone. It ends with keeping the titles' descriptions fresh.

## Watching folders

A library's **Watch for changes** is **As they happen** by default. Each node watches every folder
of every such library, and a change to a file is scanned as part of the nearest folder of the
library that is still there: a new film's folder, a season folder that gained an episode, or for a
change inside a `Subs` or `theme-music` folder, the folder that holds it.

The scan waits until the library has been quiet for a minute, and each further change moves it
later. That is long enough for a file being copied in to finish before it is read, so a film
copied over a slow network is read once, whole.

A library newly set to watch is watched within a minute. **Only when scanned** stops the watch,
and the library waits for the schedule or a scan you start.

### Where a watch sees nothing

The watch is told of changes by the operating system (inotify on Linux), one folder at a time. It
does not see:

- **Changes made by another machine on a network share** (SMB or NFS): the server's kernel is not
  told of them.
- **Folders past the inotify limit.** Each folder takes one watch. When they run out the log says "out of inotify watches:
  the rest of this library is scanned on schedule only; raise fs.inotify.max_user_watches".

For these, the schedule is the backstop, and a download tool can ask for each folder it writes
(see [scan one folder](#scan-one-folder)). To raise the limit on the host:

```sh
echo fs.inotify.max_user_watches=524288 | sudo tee /etc/sysctl.d/90-inotify.conf
sudo sysctl --system
```

In Docker the limit is the host's, so set it on the host. When the system drops events, as it
does when too many change at once, the server scans every watched library whole.

## The scan schedule

The **Scan libraries** task scans every library every 12 hours, Jellyfin's default, whether it is
watched or not. Its schedule, its last run and **Run now** are at Settings › Server › Scheduled
tasks: see [scheduled tasks](../07-maintain/04-tasks-and-jobs.md). **Scan all libraries** on
Settings › Server starts it now.

## Scan a library now

**Scan now** on a library at Settings › Server › Libraries, or **Scan library files** on its menu
in the sidebar, scans it now, and its progress shows beside it.

```sh
curl -X POST https://photon.example/api/v1/admin/libraries/$LIBRARY/scan \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

The answer is `202 Accepted`: the scan is queued and runs as soon as a node takes it. A library
never has two scans at once: asking again while one waits joins it.

### Scan one folder

A scan can be of one folder: give `path`, an absolute path inside the library, as the server sees
it. The folder it names, or the nearest folder above it that is there, is scanned with everything
under it, as Plex's `refresh?path=` does:

```sh
curl -X POST "https://photon.example/api/v1/admin/libraries/$LIBRARY/scan?path=/media/Films/Heat%20(1995)" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

That is the call for a download tool to make after it imports a file, with an
[API key](../04-access/06-api-keys.md). A path outside the library is refused. A scan asked for
over 100 folders at once reads the whole library instead, as so many changed together is an import
that reaches most of it anyway.

## What a scan reads

A scan is quick on a library where little changed, as it reads only what did:

- **A folder** whose entries are as they were at the last scan, the same names, sizes and times,
  is passed over without reading its files.
- **A file** at the size and modification time it had is not read again.
- **A new or changed file** is identified by its bytes: its size and its first and last 64 KiB.
  Only a file whose bytes the library does not already hold is probed for its streams.

Up to eight folders are read at once, and up to eight files, which suits a network or debrid
mount, where every read waits on the network.

A file that cannot be read now, as on a mount that hiccups, is left out of this scan with the
reason in the log, and its folder is read again at the next. A file that is not media at all, such
as a `.strm` naming no `http` or `https` address, is not read again until it changes.

## A file that moves or goes missing

- **Renamed or moved within the library**, a file keeps its title, and with it everyone's
  progress, marks and history, since it is known by its bytes rather than its path.
- **Removed**, a file's copy is marked missing rather than forgotten: the title stays, its copy
  labelled Missing and not offered to play. Put the file back and the next scan finds it again,
  history and all. This is deliberate: an unmounted disk, or a share down for repair, should not
  cost a household its titles. Previews of a missing copy are kept for 30 days.
- **A whole library unreadable**, its root missing or refused, fails the scan, and nothing in it
  is touched. A down mount is not an emptied library.

A title whose copies are all missing stays until you remove it. Where the library allows
[media deletion](02-libraries.md#media-deletion), **Delete…** on the title forgets it; it also
deletes any of its copies still on disk.

## Keeping descriptions fresh

A scan reads files; describing titles is the providers' work. The **Refresh metadata** task runs
every day at 3 in the morning, server time, and asks the providers again about every film and show
last matched longer ago than its library's **Refresh metadata** setting (30 days by default;
**Never** leaves them). A show with an episode announced to air is asked every 7 days, so its
calendar keeps up.

To refresh now, use **Refresh metadata** on a library or a title: see
[scan and refresh](02-libraries.md#scan-and-refresh) and
[refresh a title](05-fix-a-match.md#refresh-a-title). An NFO you edit is read at the next scan of
its folder, which the watch starts within a minute.
