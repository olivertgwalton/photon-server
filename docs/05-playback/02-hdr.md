---
title: "HDR and Dolby Vision"
summary: "How photon-server sends HDR10, HDR10+, HLG and Dolby Vision to screens that show them, and tone maps them for screens that do not."
---

An HDR copy reaches a screen that shows its HDR untouched, and a screen that does not as SDR. In between, the server does the least it can: it falls back to a range the screen does show, takes Dolby Vision down to the layer beneath it, or converts it, before it encodes anything. This page says what happens in each case, so a transcode on the dashboard reads as intended.

## What the server knows of a copy

As a library is scanned, ffprobe reads each video stream's range:

- **HDR10**: PQ, with static metadata.
- **HDR10+**: PQ with dynamic metadata on its frames, which carries an HDR10 picture.
- **HLG**.
- **Dolby Vision**: its profile, and what its base layer is without it (HDR10, SDR, HLG, an Ultra HD Blu-ray's HDR10 for profile 7, or nothing alone for profile 5).
- **SDR**: everything else.

Each player says which ranges it shows for each codec, and for Dolby Vision which profiles. The web app asks the browser and the screen: it claims HDR only where the screen is in HDR (`dynamic-range: high`) and the browser decodes a 10-bit codec with PQ or HLG, and claims Dolby Vision profiles 5 and 8 only where the browser decodes them, as Safari does. On an SDR screen it claims SDR alone, so an HDR copy is tone mapped for it.

## A screen that shows the range

A copy whose range the screen shows plays as it is. So does one whose range falls back to one the screen shows:

- **HDR10+** to a screen that shows HDR10: it shows the HDR10 underneath.
- **Dolby Vision** to a player that does not list the profile but shows the base layer: HDR10 for profiles 7 and 8.1, HLG for 8.4, SDR for 8.2. Profile 5 has no base layer to fall back to.

Where the copy goes through HLS anyway, for its container, its audio or the bitrate, its Dolby Vision is copied in one of three ways:

- **Kept**, where the player shows the profile, tagged as Dolby Vision HEVC (`dvh1`), as Apple's players need it.
- **Converted from profile 7 to 8.1**, where the player shows profile 8 but not 7: the dual-layer Blu-ray stream loses its enhancement layer and each frame's metadata (its RPU) is rewritten as 8.1, as dovi_tool does. This needs fragmented MP4 segments.
- **Stripped**, where the player shows only the base layer: the Dolby Vision metadata is removed and the base layer sent alone.

MPEG-TS segments carry no Dolby Vision, so over them a player is sent the base layer where it shows that, and an encode otherwise.

Only when none of this reaches the screen is the video encoded, with the reason **HDR**.

## A screen that does not

Encoded video keeps HDR where it can and is tone mapped where it cannot:

| Source | Encoded for a player that shows the range in 10-bit HEVC | Otherwise |
|---|---|---|
| HDR10 | HEVC Main 10, HDR10 | SDR, tone mapped |
| HDR10+ | HEVC Main 10, HDR10 | SDR, tone mapped |
| HLG | HEVC Main 10, HLG | SDR, tone mapped |
| Dolby Vision, HDR10 or Blu-ray base layer | HEVC Main 10, HDR10 | SDR, tone mapped |
| Dolby Vision, HLG base layer | HEVC Main 10, HLG | SDR, tone mapped |
| Dolby Vision 5 | SDR, tone mapped | SDR, tone mapped |

HDR is kept only in HEVC, where the server encodes HEVC and the player both plays HEVC Main 10 and shows the range; H.264 is always SDR. Kept HDR carries the source's colours and mastering metadata. Encoded video never carries Dolby Vision: its metadata describes the original picture, not the encode.

Tone mapping uses the BT.2390 curve and happens on the encoder where it can:

| Encoder | Tone mapped with |
|---|---|
| VideoToolbox | `tonemap_videotoolbox` |
| NVENC | `tonemap_cuda` |
| VA-API | `tonemap_vaapi` |
| Quick Sync | `vpp_qsv`'s own tone mapping |
| Software and AMF | `tonemapx`, on the processor |

Each server checks at start that its encoder tone maps a test picture, and keeps HDR in 10 bits where it encodes HEVC, before it takes that encoder (see [transcoding](01-transcoding.md#what-the-server-encodes-on)).

## See what a play is doing

Settings › Server › Dashboard shows each play's video as it is and as it is sent, such as `hevc 3840×2160 HDR10 → h264 1920×1080 8 Mbps tone mapped`, or `HDR10 → hevc … HDR10` where HDR is kept. Direct stream with no arrow means the HDR went as it is.

If an HDR title looks washed out or too dark on a screen, the player has claimed a range the screen does not show, or the reverse; in the web app, check that the display is in HDR mode, as the web app claims HDR only for a screen that is. See [troubleshooting](../08-reference/05-troubleshooting.md).
