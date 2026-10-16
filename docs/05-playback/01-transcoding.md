---
title: "Transcoding and hardware encoders"
summary: "When photon-server encodes video again, what it encodes on, how many at once, and how to cap the bitrate it sends."
---

Most plays need no encoding: the file goes as it is, or its video is copied into HLS. This page covers the rest: why the server encodes, what it encodes to, the GPU or processor it uses, how many videos it encodes at once, and the bitrate limits that make it encode.

## Why a play is transcoded

The server encodes a copy's video again when the player cannot take it as it is, or when copying it would break a limit. Each reason is named in the play's answer and on Settings › Server › Dashboard:

| Reason | What it means |
|---|---|
| Container | The player does not open the file's container. |
| Video codec | The player does not play the video's codec. |
| Video profile | The player does not play the video's profile. |
| Video level | The video's level is higher than the player plays. |
| Resolution | The picture is larger than the player plays. |
| Bit depth | The video's bit depth is more than the player plays. |
| HDR | The screen does not show the video's HDR (see [HDR and Dolby Vision](02-hdr.md)). |
| Audio codec | The player does not play the audio's codec. |
| Audio channels | The audio has more channels than the player plays. |
| Bitrate | The file is above the quality chosen. |
| Subtitles | The subtitles are pictures or styled text the player cannot draw, so they are drawn into the video. |
| Several files | The title is in several files, played as one. |
| Playback failed | The player tried the file as it is and failed. |

Container, audio and several-file reasons alone do not encode the video: the server copies the video into HLS and encodes the audio where it must, as Direct stream. Video reasons, the bitrate, a subtitle drawn in, and a video codec HLS cannot carry make it a transcode. Fragmented MP4 segments carry H.264, HEVC, AV1 and VP9; MPEG-TS segments H.264 and HEVC.

## What video is encoded to

- **Codec.** HEVC where the player plays it and the encoder makes it, otherwise H.264. A node whose encoder cannot make HEVC encodes H.264 alone.
- **Size.** The source's own, scaled down to the widest and tallest the player says it plays, keeping its shape.
- **Bitrate.** What the target codec needs to match the source, as Jellyfin works it out: more where the source's codec is the more efficient, more again for a small, low-bitrate source, and capped at the player's limit less its audio's share. A copy whose bitrate is not known is taken as 40 Mbps.
- **Interlacing.** An interlaced source is deinterlaced.
- **HDR.** Kept, or tone mapped to SDR; see [HDR and Dolby Vision](02-hdr.md).
- **Subtitles.** A picture subtitle (PGS, DVD) or styled text (ASS) the player cannot draw itself is drawn into the picture, which is why it forces a transcode: HLS carries neither. Styled text needs FFmpeg built with libass; the dashboard's Server card says whether it is "Available". A player asking for styled text drawn in from a server without libass is refused.

Audio is copied wherever the player takes it and HLS carries it. Otherwise it is encoded to the first codec in the player's list that the server encodes: AAC (up to 8 channels), E-AC-3 or AC-3 (up to 6). Odd layouts go to the nearest one players know, at 128 kbps a channel, or 640 kbps for 5.1 and up. Surround mixed down to stereo is made louder, so dialogue is not lost under the folded-in channels.

### Segments

HLS is cut into segments of about 6 seconds, in fragmented MP4, or MPEG-TS for a player that asks for it. Copied video is cut at the file's own keyframes, which each library reads in the background as its **Keyframes** setting says (see [add and manage libraries](../03-library/02-libraries.md)); a file whose keyframes are not known yet is cut every 6 seconds, and its keyframes are read next. Encoded video is given a keyframe every 6 seconds.

The server makes segments ahead of the player, 5 when copying and 10 when encoding, keeps the last 10 for seeking back, and stops making them after a minute with nothing asked for, giving the encoder back.

## What the server encodes on

Each server, each node where there are several, finds its encoder as it starts. It tries, in order:

1. VideoToolbox, on a Mac.
2. NVENC, on the first NVIDIA card.
3. On Linux, Quick Sync (QSV), then VA-API, on each render node under `/dev/dri`. Quick Sync is faster on Intel; only VA-API drives AMD.
4. On Windows, Quick Sync on an Intel GPU, then AMF on an AMD one.

It takes the first that encodes a test picture through the whole chain a title would take (deinterlaced, tone mapped, and in 10 bits for HEVC), so a missing device or driver is found at start rather than at the first play. Where none does, it encodes in software with x264 and x265. It then checks HEVC, and drawing a subtitle in. Its log says what it chose, in the line `encoding video`, with `on`, `device` and `hevc` (`allow` or `deny`); a warning `encoding H.264 alone` says why HEVC was not.

Settings › Server › Dashboard shows it under Server, as **Encoder**: "VA-API, /dev/dri/renderD128, HEVC and H.264", or "H.264 only". In a container, the GPU's devices must be passed in; see [install with Docker](../01-get-started/02-install-docker.md#hardware-transcoding).

### Choose the encoder yourself

Where the automatic choice is not the one you want, such as a second GPU, set it:

1. Open Settings › Server › Dashboard.
2. In the Server card, open the actions menu (`…`) of the node and choose **Settings…**.
3. Set **Encoder** to Automatic, Software, VideoToolbox, VA-API, Quick Sync, NVENC or AMF. For VA-API and Quick Sync, give the render node, such as `/dev/dri/renderD129`; for NVENC, the card's index, such as `1` for the second. Left empty, the first is used.
4. Save.

The node tries it at once and encodes the next video it starts on it; one already encoding keeps what it began on, so a seek never changes it. An encoder that fails the test picture is passed over for the first that passes, and the log says so.

```sh
NODE=$(curl -s https://photon.example/api/v1/admin/server \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq -r .node_id)
curl -X PATCH "https://photon.example/api/v1/admin/nodes/$NODE" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"encoding": {"source": "set", "acceleration": "vaapi", "device": "/dev/dri/renderD129"}}'
```

`{"encoding": {"source": "automatic"}}` goes back to the automatic choice.

## How many videos are encoded at once

**Transcodes at once**, in the same Settings… dialog, is one of:

| Setting | What it does |
|---|---|
| Worked out from its encoder | The default. In software, one transcode per four logical CPUs, at least one; on any hardware encoder, 8. |
| At most | The number you give. |
| No limit | As many as are asked for. |

Eight is the number of NVENC sessions a GeForce card's driver allowed at once before driver 591.44; later drivers allow 12 per machine, which a limit set to 12 takes up. The other encoders are bound by how fast they encode rather than a count, and reach about that many at 1080p. Lowering the limit stops nothing playing; nothing more begins until there is room.

A download's conversion holds a transcode slot too, but gives it up to any play that needs it (see [downloads](03-downloads.md)). The dashboard's Now playing heading says "3 of 8 transcoding · 1 for downloads".

When every slot is taken, a play that needs encoding is refused with `503` and `transcode_limit`, saying how many the server encodes at once. A server with less than 2 GiB free in its cache folder refuses a new transcode the same way, as segments are written there. On several servers, the next node is asked first; see [several servers](04-several-servers.md#where-a-transcode-is-made).

## Cap the bitrate

Two limits decide the most a play may be sent, and the lower wins:

- **Quality**, in each person's Settings › Playback: Original (the file as it is), or up to 40, 20, 10, 8, 6, 4, 3, 2 or 1.5 Mbps, 720 or 420 kbps. Above it, the server converts the file down. The player's own quality menu overrides it for that title.
- **Remote streams**, in Settings › Server › Network: **Limit (Mbps)**, the most a stream to anyone outside the server's **Local networks** is sent at, so playing away from home leaves the upload room. `0` is no limit, the default.

```sh
curl https://photon.example/api/v1/admin/network -H "Authorization: Bearer $ADMIN_TOKEN"
```

returns the network settings; send them back with `PUT` and `remote_max_bitrate_kbps` changed, such as `8000` for 8 Mbps. A copy above the limit is transcoded to fit it; its picture keeps the size the player asked for. See [remote access](../04-access/04-remote-access.md) for the local networks.
