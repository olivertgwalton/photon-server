---
title: "Intros, credits and timeline previews"
summary: "How the server finds the intros and credits a player offers to skip, makes chapter images and seek previews, and when it does the work."
---

The server reads a library's files for two things a player uses: **markers**, the stretches of a
copy a player may offer to skip (an intro, a recap, the credits, a preview of the next episode),
and **previews**, a picture of each chapter and a sheet of thumbnails to show while seeking. Each
library says how much of this it wants, and the server's maintenance window says when the work is
done. This page covers both, and how to correct a marker by hand.

## Choose what a library looks for

Both are in a library's settings, at Settings › Server › Libraries, the library, **More
settings**.

| Setting | What it does |
| --- | --- |
| Previews | **Chapters and seeking** makes a picture of each chapter and seek previews (the default). **Chapter images only** makes the chapter pictures alone. **None** makes neither, and removes those made. |
| Intros and credits | **From chapters, sound and picture** reads chapter names, compares each season's sound, and finds where each film's picture goes dark for its credits (the default). **From chapters only** reads chapter names and asks plugins. **Not looked for** offers only markers an admin set. |

A library streamed by a provider starts with both at None and Not looked for.

```sh
curl -X PATCH https://photon.example/api/v1/admin/libraries/$LIBRARY \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"previews": "chapters", "markers": "all"}'
```

`previews` is `all`, `chapters` or `off`; `markers` is `all`, `chapters` or `off`.

## Where markers come from

A copy has at most one marker of each kind in each of its parts. Where several sources have one,
the most trusted the library takes is used:

1. **Set here**: an admin's, which always counts (see Correct a marker below).
2. **From a chapter**: a chapter whose title names it, such as "Intro", "Opening", "Credits",
   "Recap", "Previously" or "Next time", as the Intro Skipper plugin reads them. Its author marked
   it to the frame.
3. **From a plugin**: a plugin with the Intros and credits capability, such as one serving a
   database of intros timed by hand (see [plugins](../06-integrations/01-plugins.md)).
4. **Found by sound**: what a season's episodes share.
5. **Found by picture**: a film's credits.

A marker from a chapter, by sound or by picture is at least 15 seconds long, and at most 2
minutes for an intro, recap or preview, or 15 minutes for the credits: a longer stretch is more
likely a scene two episodes share than an opening. From chapters, the first intro and recap of a
part count, and the last credits and preview.

Recaps and previews come only from chapters, plugins and admins; the server finds intros and
credits itself.

### Comparing sound

Under **From chapters, sound and picture**, the server fingerprints the sound of each episode and
compares it with the other episodes of its season, as Plex's intro detection and Jellyfin's Intro
Skipper do. The intro is looked for in the first quarter of an episode, at most its first 10
minutes, or the whole of an episode shorter than 5 minutes; the credits in its last 7½ minutes.
What an episode shares with another for at least 15 seconds is taken for its intro or credits.

This needs an FFmpeg built with Chromaprint. The Docker image's has it; Homebrew's does not.
Settings › Server › Dashboard says under **Chromaprint** whether the server has it, and so does
`chromaprint` in `GET /api/v1/admin/server`. Without it, seasons wait uncompared until the server
has an FFmpeg that can.

A season is compared 10 minutes after its last episode arrives, so a season scanned folder by
folder is compared once. An episode added later has its season compared again; episodes already
compared keep their markers.

### Reading a film's picture

A film has no other episode to compare with. Its last part's final 15 minutes are read for dark
scenes with lettering in most of their frames: the credits run from the first such scene to the
last, or to the end where the last ends within 20 seconds of it.

### Asking plugins

Where a plugin times intros and credits, each film and episode is asked about once, a film by its
ids and an episode by its show's ids and its season and episode numbers, with the copy's length.
A copy in several parts is not asked about, as a timing is of a whole release. Libraries set to
**Not looked for** are not asked about.

## Correct a marker

1. Open the title, then its More menu (⋯) › Admin › **Edit markers**.
2. Each marker shows its kind, where it **Starts** and **Ends** on the copy's whole timeline, and
   where it came from. Change the times, take one out with its cross, or **Add a stretch**.
3. Under **Has none**, tick a kind a part does not have, so nothing found by sound or picture is
   used for it.
4. Choose **Save markers**.

What you set outranks every other source. Saving with nothing listed and nothing ticked gives the
copy back to what was found. A title with several copies has a Markers card for each.

```sh
curl -X PUT https://photon.example/api/v1/admin/versions/$VERSION/markers \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"markers": [{"kind": "intro", "start_ms": 62000, "end_ms": 151000}],
       "absent": [{"kind": "credits", "part": 0}]}'
```

`$VERSION` is a copy's `id` among the title's `versions`. A marker must lie within one part of
the copy; `part` counts from 0.

Turning a library's detection down keeps what was found: switched back up, the markers found
before are offered again without reading the files a second time.

## What players do with them

A copy's markers are on its title page, under each version's `markers`, with where each came
from. Jellyfin apps read them as media segments (see [Jellyfin apps](../04-access/05-jellyfin-apps.md)).

Whether a player skips is the profile's choice, at Settings › Playback: **Intros and recaps**, and
**Credits and previews**, each **Show a button to skip** (the default), **Skip by itself** or
**Don't offer to skip**.

## Previews

Under **Chapters and seeking**, each part of a copy gets:

- **Seek previews**: a thumbnail 320 pixels wide every 10 seconds, a hundred to a sheet, as
  Jellyfin makes them.
- **Chapter images**: a picture 640 pixels wide at the start of each chapter. A chapter at the
  very start is pictured 15 seconds in, or halfway through where it is shorter, as its first frame
  is often black.

A picture of an HDR video is tone mapped. Each try at a chapter's picture has 10 seconds, 20 from
HDR; the first that cannot be made stops the rest, keeping those made before it, so a mount
that has stopped answering does not cost every chapter its limit.

Previews are kept where the server keeps them (see [storage](../07-maintain/05-storage.md)). A
part whose file goes missing keeps its previews for 30 days, so a share down for repair does not
cost hours of remaking.

A client reads a part's sheets from its layout: the sheet for a time is the time, divided by the
interval, divided by columns times rows, rounded down, counted from 0.

```sh
curl https://photon.example/api/v1/parts/$PART/trickplay \
  -H "Authorization: Bearer $TOKEN"
```

```json
{"width": 320, "height": 180, "interval_ms": 10000, "columns": 10, "rows": 10, "thumbnails": 642, "sheets": 7}
```

Each sheet is a JPEG at `/api/v1/parts/{id}/trickplay/{sheet}`.

## When the work is done

Reading files for markers and previews waits for the maintenance window, set at Settings › Server
› Scheduled tasks (see [scheduled tasks and jobs](../07-maintain/04-tasks-and-jobs.md)). It is from
02:00 to 05:00 UTC unless changed. Nothing that reads the libraries' files in the background
starts while anything plays.

| Setting | What it does |
| --- | --- |
| Make previews | **In the window** (the default), or **In the window and as titles are added** |
| Find intros and credits | **In the window**, or **In the window and as titles are added** (the default) |

Two scheduled tasks run as the window opens:

- **Detect intros and credits** queues every season with an episode not yet compared, every film
  whose end has not been read, and, where a plugin times them, every film and episode not yet
  asked about.
- **Make previews** queues every part whose previews are not what its library asks for, among
  them a library's whose setting changed, forgets previews of parts missing for 30 days, and
  clears those no part has any more.

So a change to a library's Previews or Intros and credits reaches its files at the next window.
**Run now** on either task queues the work to start at once, outside the window, and run to its
end.

```sh
curl -X POST https://photon.example/api/v1/admin/tasks/detect_markers/run \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

The task for previews is `backfill_previews`. Progress shows on the task, and each title's work
in Settings › Server › Jobs as **Find intros and credits** and **Make previews**.
