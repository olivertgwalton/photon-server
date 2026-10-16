---
title: "Downloads and conversions"
summary: "How a film or episode is kept on a device, as its own file or converted down to a size, and how the server makes and clears conversions."
---

Anyone signed in can download a film or an episode to keep on a device: the file as it is, or a smaller copy the server converts for a phone or a slow connection. This page covers asking for one, how the server makes conversions without getting in the way of playback, and how long they are kept.

## Download a title

In the web app, choose **Download…** from a film's or an episode's menu. The dialog offers:

| Quality | What is asked for |
|---|---|
| Original | The file as it is. |
| 1080p, 20 Mbps | No wider than 1920, no more than 20 Mbps. |
| 1080p, 8 Mbps | No wider than 1920, no more than 8 Mbps. |
| 720p, 4 Mbps | No wider than 1280, no more than 4 Mbps. |
| 480p, 1.5 Mbps | No wider than 854, no more than 1.5 Mbps. |

A copy in several files is downloaded a file at a time: the dialog's **Files** offers all of them, each a download of its own, or one part.

The download then appears under **Downloads** in the sidebar, as Waiting, Converting (with how far it has got), Ready or Failed. **Save** fetches a ready one, and the cross removes it.

## The file as it is, or a conversion

A copy already within the quality asked for, no more than its bitrate and no wider, is its own file: the download is ready at once and costs the server nothing. A copy whose bitrate is not known is taken as 40 Mbps, so it is its own file only at Original.

Any other copy is converted, into one MP4 with its index at the front so a player can start it before it has all of it:

- **Video** is HEVC where the device says it plays HEVC and the server encodes it, otherwise H.264, within the bitrate and width. HEVC keeps HDR10 or HLG where the device says it shows it; anything else is tone mapped to SDR, as a transcode is (see [HDR and Dolby Vision](02-hdr.md)). The web app names no codecs, so its conversions are H.264 in SDR, which every device plays.
- **Audio** is AAC, within its share of the bitrate.

A conversion is made once for every profile and device that asks for the same file at the same bitrate, width, codec and range, and asking again for a download you already have answers the same one.

```sh
curl -X POST https://photon.example/api/v1/downloads \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"title_id": "0190a3c2-…", "max_bitrate_kbps": 8000, "max_width": 1920,
       "video_codecs": ["hevc", "h264"], "video_ranges": ["sdr", "hdr10"]}'
```

The answer is `201` for a new download and `200` for one you already had. A copy in several files needs `part_id` to say which. `GET /api/v1/downloads` lists the device's downloads, or with `scope=profile` the profile's on every device. A ready download carries `url`, a signed address good for 24 hours, served in byte ranges so a download manager can resume it.

## How the server makes conversions

Each conversion is a job, **Convert for download**, which you can follow on Settings › Server › Jobs. A server makes one conversion at a time, so a playback is never starved of the processor, and each holds one of its transcode slots (see [transcoding](01-transcoding.md#how-many-videos-are-encoded-at-once)) while no playback needs it:

- A playback that needs a slot takes the conversion's. The conversion stops and starts again from the beginning once a slot is free. The dashboard shows conversions apart: "3 of 8 transcoding · 1 for downloads".
- Conversions do not wait for nothing to be playing, or for the maintenance window: someone is waiting on each, and one held back by every evening's viewing would not be ready for days.
- A node set to serve only, or drained, makes none; another node does. See [several servers](04-several-servers.md).
- A conversion whose FFmpeg gets no further for 5 minutes, as on a network mount that stopped answering, fails. One FFmpeg cannot make is marked Failed with its reason and not tried again until someone asks for it again.

The converted file is kept in the cache folder of the node that made it, under `downloads`. Any node serves it, fetching it from the node that holds it; while that node is not running, the file cannot be fetched.

## How long downloads are kept

A download stays for 7 days once it is ready, or once its conversion failed, unless it is removed first: a week covers a device away or switched off before it fetches. The scheduled task **Clear old downloads** forgets those past their time every hour, and the conversions no download needs; each node removes the files of those every 10 minutes. Removing a download stops its conversion and removes its file where no other download needs it.

```sh
curl -X DELETE https://photon.example/api/v1/downloads/0190a3c2-… -H "Authorization: Bearer $TOKEN"
```

Removing a download only forgets it on the server: a file already saved on a device stays there.
