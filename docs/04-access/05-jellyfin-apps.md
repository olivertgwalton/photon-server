---
title: "Use Jellyfin apps"
summary: "Let Infuse, Swiftfin, Findroid and Jellyfin's own apps sign in to photon-server on a port of their own."
---

photon-server can answer Jellyfin's API as well as its own, so the apps made for Jellyfin can use
it: Infuse, Swiftfin, Findroid and Jellyfin's own apps, and the TV apps for LG and Samsung, which
load Jellyfin's web app from the server. It answers as Jellyfin 12.2, on a port of its own, and
underneath it is the same server: the same profiles, the same access, the same progress, favourites
and playlists, and the same decision on how each copy plays.

## Turn it on

1. Open Settings › Server › Network.
2. Under **Jellyfin apps**, set **Jellyfin apps** to **On**. It is off by default.
3. Leave **Port** at 8096, Jellyfin's own, where its apps look first, or choose another from 1 to
   65535. It cannot be 8640, the port photon's own API is served on.
4. Save. Every node starts listening on the port at once.

```sh
curl https://photon.example/api/v1/admin/network -H "Authorization: Bearer $ADMIN_TOKEN"
```

The answer's `jellyfin` and `jellyfin_port` are these settings; change them with a `PUT` of the
whole network, as [Reach your server from outside](04-remote-access.md) describes. Where the node
answering cannot listen on the port, because another program holds it, Settings › Server › Network
says why under **Port**, and the answer carries it as `jellyfin_error`. The node tries again every
minute.

In Docker, publish the port too, beside 8640, under `server:` in `compose.yml`:

```yaml
    ports: ["8640:8640", "8096:8096"]
```

The Docker image and the release archives carry Jellyfin's web app, which the server serves at
`/web/` on that port, as Jellyfin does; opening the port's root in a browser goes there.

## Add the server in an app

In the app, add a server at `http://<server>:8096`, or `https://` where the server has a
certificate (see below). The app shows the server by the **Name** under Settings › Server ›
General.

Jellyfin apps look for servers on the local network by asking on UDP port 7359, which photon-server
does not answer, so type the address.

Sign in with a profile's name and password. The server lists no profiles to whoever asks, so the
app shows its form for a name and password rather than a row of faces. A sign-in here is checked,
limited and logged as one in the web app is: the same attempts per address and per name, the same
entry in the [activity log](07-devices-and-playback.md#the-activity-log).

## Quick Connect

Quick Connect is Jellyfin's name for [pairing](03-pair-a-tv.md), and is always on. The app shows a
six-digit code, since that is all Jellyfin's apps take; approve it under Settings › Link a device in
the web app, or under Quick Connect in a Jellyfin app already signed in. The app signs in as the
profile that approved it.

A profile made by a [sign-in provider](02-single-sign-on.md) has no password until it sets one, so
Quick Connect is how it signs in to a Jellyfin app.

## HTTPS

The Jellyfin port is served as photon's own port is: with a certificate set under
Settings › Server › Network, it answers HTTPS beside plain HTTP, with the same certificate. Secure
connections **Required** sends plain HTTP to HTTPS on photon's own port only; the Jellyfin port
still answers plain HTTP. Behind a reverse proxy, give the Jellyfin port a site of its own, and the
**Trusted proxies** setting applies to it as well.

**Limit (Mbps)**, the remote stream limit, applies to Jellyfin apps outside the local networks too.

## What a Jellyfin app can and cannot do

A Jellyfin app browses, searches, plays, downloads a title's file as it is, marks titles watched
and favourite, keeps playlists, and shows a title's chapters, trickplay pictures, intro and credits
markers and theme music.

Some of Jellyfin is not there, deliberately:

- **No profile is a Jellyfin administrator**, admins included, since an app would offer Jellyfin's
  dashboard, which is not served. Run the server from photon's own web app.
- **Live TV, SyncPlay, sharing and deleting** are turned off, so apps do not offer them: photon
  has no part of them.
- **Intros** before a title are never played.
- **A person is opened by id, not by name**, since two people here may share a name.

## Signing a Jellyfin app out

A Jellyfin app holds a token like any other device, and is listed under Settings › Devices by the
device name and app it gave. Sign it out there, or from the app. See
[Signed-in devices](07-devices-and-playback.md).
