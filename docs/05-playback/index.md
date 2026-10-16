---
title: "Playback"
summary: "How photon-server decides what each player is sent, and the pages on transcoding, HDR, downloads and running several servers."
---

Every time a film or an episode starts, the app playing it sends the server a profile: the containers it opens, the video and audio codecs it decodes and how far, the dynamic ranges its screen shows, the subtitle formats it draws itself, and the most it wants to be sent. The server compares that with the copy it is asked to play and chooses one of three ways to send it.

| Method | What the server does | Shown on the dashboard as |
|---|---|---|
| `direct` | Sends the file as it is, in byte ranges. | Direct play |
| `remux` | Copies the video into HLS segments, encoding only the audio where the player cannot take it. | Direct stream |
| `transcode` | Encodes the video again, to HEVC or H.264, into HLS. | Transcode |

A copy plays directly only where the player opens its container and plays every stream in it, the subtitle asked for included, within the bitrate it allows. Anything less and the server says why, in the play's `reasons`, and Settings › Server › Dashboard shows them under each play in Now playing ("Because of: Audio codec, Bitrate"). Photon, the web app and Jellyfin apps all go through the same decision.

## Pages

- [Transcoding and hardware encoders](01-transcoding.md): when video is encoded again, what it is encoded on, how many at once, and the bitrate limits.
- [HDR and Dolby Vision](02-hdr.md): what happens to HDR10, HDR10+, HLG and Dolby Vision on screens that show them and screens that do not.
- [Downloads and conversions](03-downloads.md): keeping a copy on a device, as it is or converted down to a size.
- [Run several servers](04-several-servers.md): nodes sharing one database, and where each stream is made.

What a person chooses for themselves, their quality, languages and subtitles, is in [your settings](../02-web-app/06-your-settings.md). When something does not play, see [troubleshooting](../08-reference/05-troubleshooting.md).
