---
title: "Reach your server from outside"
summary: "Serve HTTPS with your own certificate or behind a reverse proxy, set the public address, say which networks are home, and limit streams sent away from it."
---

At home, apps reach photon-server at its own address on port 8640. To reach it from anywhere else,
you put it at an address the internet can reach, over HTTPS, either with a certificate the server
serves itself or behind a reverse proxy that holds the certificate. Everything on this page is under
Settings › Server › Network, and every node of the server takes up a change at once.

## Settings › Server › Network

| Setting | What it does |
|---|---|
| Secure connections | **Required**, **Preferred** or **Disabled** (the default). |
| Certificate | The path, on the server, of a PEM certificate chain. Required and Preferred need it. |
| Key | The path of the certificate's key. |
| Public address | Where people reach the server from outside, such as `https://photon.example.com`. Empty by default. |
| Trusted proxies | Reverse proxies whose `X-Forwarded-For` says who a device is, as prefixes or addresses. None by default. |
| Discovery | **On** (the default) answers apps looking for a server on the local network. |
| Jellyfin apps | Whether apps made for Jellyfin can sign in, and on which **Port**. See [Use Jellyfin apps](05-jellyfin-apps.md). |
| Local networks | The networks whose devices are at home, as prefixes or addresses. Empty, every private network is. |
| Limit (Mbps) | The most a stream to a device outside the local networks is sent at. 0, the default, is no limit. |

```sh
curl https://photon.example/api/v1/admin/network -H "Authorization: Bearer $ADMIN_TOKEN"
```

`PUT` to the same address replaces every setting at once, so send back what you read with your
change in it.

## Serve HTTPS from the server itself

The server answers HTTPS and plain HTTP on the same port, 8640, as Plex's does: it tells them apart
by a connection's first byte.

1. Get a certificate for the server's name: certbot, Caddy or Tailscale write a PEM chain and key
   that work as they are.
2. Make the two files readable by the server at the same paths on every node: in Docker, mount
   them into the container.
3. Under Settings › Server › Network, set **Certificate** and **Key** to their paths, and
   **Secure connections** to **Preferred** or **Required**. The server refuses to save a
   certificate and key it cannot read.

**Preferred** answers both HTTPS and plain HTTP. **Required** sends a browser that comes over plain
HTTP to HTTPS at the **Public address**, but one on the server's own machine, so a script there
keeps working; with no HTTPS public address set, a plain request is refused instead. **Disabled**
answers plain HTTP alone.

The server reads the files again every minute, so a certificate renewed in place is served within a
minute with nothing restarted. A file caught half-written leaves the node serving the certificate
it had.

## Serve HTTPS behind a reverse proxy

Behind a proxy that holds the certificate, such as Caddy, nginx or Traefik, leave **Secure
connections** disabled, and:

1. Point the proxy at the server's port 8640, passing WebSocket upgrades through: the web app keeps
   one open at `/api/v1/events`.
2. Have the proxy send `X-Forwarded-For` and `X-Forwarded-Proto`.
3. Add the proxy's address, or the network it is on, to **Trusted proxies**: `172.16.0.0/12` covers
   Docker's default networks.

Without a trusted proxy the server believes no `X-Forwarded-For`, so every device behind the proxy
looks like the proxy, and is counted local if the proxy is. A client cannot name its own address by
sending the header: it is believed only from a trusted proxy, and only back to the first hop that is
not one. `X-Forwarded-Proto: https` from a trusted proxy is how the server knows a browser came over
HTTPS, which decides whether its sign-in cookie is marked secure.

Set the server up from its own address before you put a proxy in front of it: the
[first-run setup](../01-get-started/04-first-run.md) is open only to a device on the local network
reaching the server directly.

## The public address

**Public address** is where people reach the web app from outside, over `http` or `https`. The
server uses it:

- as the address a television shows to [pair](03-pair-a-tv.md) at, so a phone away from home
  reaches it;
- for [single sign-on](02-single-sign-on.md), which needs one: the provider sends browsers back to
  it;
- to send plain HTTP to HTTPS where secure connections are required.

Left empty, each device is answered at the address it used.

## Local networks and the remote limit

A device is **local** when its address is in **Local networks**, or on the server's own machine.
Left empty, every private network is local: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, their
IPv6 equivalent and link-local addresses. Tailscale's `100.64.0.0/10` is not private, so add it to
treat a tailnet as home; adding any network replaces the private ones, so list your home network
beside it.

Being local matters in three places:

- **Limit (Mbps)** applies only to devices that are not local. Above it the video is encoded to fit,
  as Jellyfin's internet streaming bitrate limit does, so playing away from home leaves your upload
  room; the picture keeps the size the app asks for. See [transcoding](../05-playback/01-transcoding.md).
- A forgotten password is [reset](01-profiles.md#a-forgotten-password) only from a local device.
- The [first-run setup](../01-get-started/04-first-run.md) is open only to a local device.

## Discovery

With **Discovery** on, the server answers apps on the local network that broadcast
"who is PhotonServer?" on UDP port 8640, with its name and the address to reach it at, over HTTPS
where it serves HTTPS. It answers only askers on a private network, since an answer is several times
the size of the question and answering anyone could lend the server to an amplification attack.
In Docker, a broadcast reaches the container only on host networking; elsewhere, give apps the
address yourself.
