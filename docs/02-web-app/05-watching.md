---
title: "Watching in the browser"
summary: "The web app's player: its controls and keys, choosing tracks and subtitles, skipping intros and credits, and downloading a copy to keep."
---

Press **Play** on a title's page, or **Play** or **Resume** in a card's menu, and the player fills
the window. This page covers what it does and every control it has, then downloading a copy.

## How a title reaches the browser

Before it plays, the player tells the server what this browser can play (its containers, video and
audio codecs, and whether the screen shows HDR) and the most it should send, your **Quality**. The
server answers with one of three ways, named as **Playback info** names them:

- **Direct play**: the file as it is.
- **Direct stream**: the file's tracks copied, as they are, into a stream the browser opens.
- **Transcode**: encoded again by the server, to what the browser plays or down to the quality
  asked.

The server prefers them in that order, so a title is converted only where it has to be; the
reasons it gives are listed under **Why** in **Playback info**. How the server decides, and the
hardware it converts with, are in [transcoding](../05-playback/01-transcoding.md) and
[HDR](../05-playback/02-hdr.md).

A file that the browser said it would play but then fails on is asked for again, converted. A
stream that breaks is opened again where it was, once a minute at most. Where the server refuses,
the player says **Can't play this here** with the reason, such as every transcoder being busy, and
offers **Try again** and **Back**.

A title opens where you stopped. The player tells the server where it has got to as it plays and
when you leave, so Continue Watching, Next Up and your other devices pick up from there. Closing
the tab stops the playback on the server too.

## The controls

Move the pointer or press a key to show the controls; they hide after three seconds of playing.

| Control | What it does |
| --- | --- |
| Back arrow | Leaves for the title's page. |
| Play, Pause | Also a click on the picture. A double-click toggles full screen. |
| Previous | The title before, in a playlist or the show; at the first, back to the start. |
| Back 10 seconds, Forward 10 seconds | |
| Next | The next title, in a playlist or the show. |
| Mute and Volume | This browser remembers the volume. |
| Subtitles | Turns subtitles on, as last chosen, or off. |
| Settings | The menu below. |
| Play on another screen | AirPlay in Safari, or Cast in Chrome on Android, where the browser offers one. It sends only a stream the browser plays by itself. |
| Picture in picture | Where the browser has it. |
| Full screen | |

The seek bar shows what has played and what has arrived, the chapters as ticks, and a picture of
wherever the pointer is: from the title's timeline previews, else its chapter's picture (see
[markers and previews](../03-library/09-markers-and-previews.md)).

The keyboard's media keys and the system's own controls (a lock screen, headphones) work while the
player is open.

### Keys

| Key | What it does |
| --- | --- |
| Space or K | Play or pause |
| Left, Right | Back or forward 5 seconds |
| J, L | Back or forward 10 seconds |
| Up, Down | Volume |
| M | Mute |
| F | Full screen |
| C | Subtitles on or off |
| Shift+N, Shift+P | Next or previous title |
| 0 to 9 | Jump to that tenth of the title |

### The settings menu

| Item | What it does |
| --- | --- |
| Audio | The copy's sound tracks, where it has more than one. Changing it opens the title again where it was. |
| Subtitles | **Off**, or any subtitle in the file or beside it. |
| Subtitle timing | Shows the subtitle earlier or later, by 0.1 or 1 second, or **On time**. Another subtitle starts on time. |
| Quality | **Original** (the file as it is), or a ceiling from 40 Mbps down to 420 kbps. What you choose here becomes your profile's Quality on every device. |
| Speed | 0.5× to 2×, **Normal** by default. |
| Picture | **Fit**, **Crop to fill** or **Stretch**, for a screen of another shape. |
| Chapters | Jumps to a chapter, where the copy has them. |
| Playback info | How the title is reaching you, why, and what the file, video, audio and dropped frames are. |

## Choosing a copy and tracks

A title's page offers **Version**, **Audio** and **Subtitles** under Play, where there is more than
one answer. What you leave alone, the server chooses for your profile: a copy, then the sound and
subtitles your [playback settings](06-your-settings.md#playback) ask for, or the ones you last
chose for that title where your profile remembers them.

The player's address carries the same choices, so a link can open a title at a moment with given
tracks: `/play/{id}?t=90` starts at 90 seconds, and `version`, `audio` and `subtitle` (a track's
index, or `off`) choose the rest.

## Subtitles

Every kind of subtitle shows in the browser; how depends on its kind:

- **Text** (SubRip, WebVTT and the like): a file beside the copy is shown by the browser as it is.
  A track inside the file is carried to the browser by the server as WebVTT, so a title that would
  have played as it is is streamed instead while one is on.
- **Styled** (ASS, SSA): drawn by the browser as written, with the fonts the file carries.
- **Pictures** (PGS from Blu-ray, VobSub from DVD): drawn by the browser over the video.
- Any other picture subtitle is drawn into the video by the server, which converts the title to do
  it.

How plain text subtitles look (size, colour and background) is set for each browser under
[Subtitles on this device](06-your-settings.md#subtitles-on-this-device). Styled and picture
subtitles keep the look they were made with.

### Find subtitles

**Find subtitles…**, on a film's or an episode's page or card, searches the subtitle providers an
admin has set up, in your subtitle language, else your browser's; **Language** searches in another.
A result made for that very file is starred and listed first. **Add** keeps it beside the copy, for
everyone in the household, and it is offered from then on. Setting up providers is in
[subtitles](../03-library/10-subtitles.md).

## Skipping intros and credits

Where the server knows where an intro, recap, the credits or a preview is, the player offers
**Skip Intro**, **Skip Recap**, **Skip Credits** or **Skip Preview** over the picture while it
plays. Your [playback settings](06-your-settings.md#playback) choose, for intros and recaps and for
credits and previews, whether it shows that button, skips by itself, or offers nothing. A section
skipped by itself is skipped once: seek back into it and it plays.

Skipping credits that run to the end of an episode goes straight on to the next episode.

## Up next

As the credits of an episode start, or 30 seconds before the end where the server has not found
them, **Up next** shows the next episode in the show or the next title in a playlist: **Play now**
plays it, **Hide** puts it away. Where [Play the next episode](06-your-settings.md#playback) is on,
it plays by itself after a ten-second count, which holds while the title is paused; off, it waits
for you.

## Download a copy

**Download…**, from a film's or an episode's page or card, asks the server for a copy to keep:

| Quality | What you get |
| --- | --- |
| Original | The file as it is. |
| 1080p, 20 Mbps | At most 1920 wide and 20 Mbps. |
| 1080p, 8 Mbps | At most 1920 wide and 8 Mbps. |
| 720p, 4 Mbps | At most 1280 wide and 4 Mbps. |
| 480p, 1.5 Mbps | At most 854 wide and 1.5 Mbps. |

A copy already within the quality chosen comes back as itself; anything larger is converted by
the server, to H.264 in SDR, which plays nearly everywhere. A copy in several files offers **Files**:
all of them, each its own download, or one. Choose the copy beforehand with **Version** on the
page; **Download…** on a card asks which, where there are several.

```sh
curl -X POST https://photon.example/api/v1/downloads \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"title_id": "…", "max_bitrate_kbps": 8000, "max_width": 1920}'
```

**Downloads** in the sidebar lists what this browser asked for, the newest first. Each says its
quality and its state: **Waiting**, **Converting** with how far it has got, **Ready**, or **Failed**
with why. A ready one has **Save**, which saves the file; the cross removes it. Asking for the same
title at the same quality again is the same download, not a second one.

A download stays a week once it is ready or has failed, unless you remove it first, so a converted
file does not hold the server's disk for good. How conversions are made and kept is in
[downloads and conversions](../05-playback/03-downloads.md).
