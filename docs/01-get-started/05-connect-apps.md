---
title: "Connect Photon and other apps"
summary: "Sign in to the server from a browser, from Photon on Apple devices, and from apps made for Jellyfin."
---

Once the server is [set up](04-first-run.md), each person signs in on their own devices with their
own profile. This page covers the web app, Photon on iPhone, iPad, Mac, Apple TV and Apple Vision
Pro, and the apps made for Jellyfin. Every device signed in is listed for its profile under
Settings › Devices, and can be signed out there.

## The web app

The web app is the server itself: open the server's address in a browser, such as
`http://192.168.1.20:8640` at home or your [public address](../04-access/04-remote-access.md)
away from it. **Log in** asks for the profile's name and password, and offers a button for each
[single sign-on](../04-access/02-single-sign-on.md) provider an admin has added. See
[Use the web app](../02-web-app/index.md).

## Photon

[Photon Player](https://apps.apple.com/gb/app/photon-player/id6804291311) is the App Store app for
iPhone, iPad, Mac, Apple TV and Apple Vision Pro. It plays from several kinds of media server, and
photon-server is the one it calls **Photon Server**.

### Add the server

1. In Photon, open Settings › Media Servers › Add Server, or, before any source is added,
   **Add a Media Server** on the welcome screen.
2. Choose **Photon Server**.
3. Pick the server under **On This Network**, or type its address in **Address**, such as
   `http://192.168.1.20:8640`, and choose **Connect**. An address with no scheme is tried over
   HTTPS, so type `http://` for a server that serves plain HTTP.
4. Sign in with your profile's name and password.

Photon checks the address at `GET /api/v1/server` before it asks for a password, so a typing
mistake is caught before you sign in. The first media server in Photon is free; a second needs
Photon Pro.

### Found on the network

Photon lists the servers that answer on the local network. It broadcasts `who is PhotonServer?` on
UDP port 8640, and the server answers with its id, name and version and the address to reach it
at, over HTTPS where it serves it. Photon lists each server once, by its id, however many addresses
it answers from.

The server answers only devices on the same machine or a private network, so it never answers the
internet. It does not answer when:

- Settings › Server › Network › Discovery is Off. It is On on a new server.
- It runs in Docker on Docker's own network: a broadcast does not reach the container. Type the
  address.
- The device is on another network, such as a guest Wi-Fi or a VPN that does not pass broadcasts.

You can check it from a Linux or Mac machine on the same network:

```sh
echo -n 'who is PhotonServer?' | nc -u -w1 192.168.1.20 8640
```

### Sign in on Apple TV

A television has no comfortable keyboard, so Photon on Apple TV pairs with a code instead of a
password:

1. Add the server on the Apple TV as above. Photon shows a code, such as `BCDF-GHJK`, and the
   address to enter it at.
2. On a phone or computer signed in to the server, open that address, or Settings › Link a device
   in the web app.
3. Enter the code under **Code** and select **Link**.

The Apple TV signs in as the profile that linked it, a few seconds later. A code lasts ten
minutes. The address the TV shows is the server's public address followed by `/link`, or the
address the TV reached it at where no public address is set. See
[Sign in on a TV](../04-access/03-pair-a-tv.md).

To approve a code from the API, signed in as the profile the TV should be:

```sh
curl -X POST https://photon.example/api/v1/auth/pairings/approvals \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"user_code": "BCDF-GHJK"}'
```

### Switch profile

A device signed in to one profile can switch to another without signing in again: in Photon,
Settings › Media Servers, the server, then **Active Profile**. A profile with a PIN or a password
asks for it. See [Profiles, passwords, PINs and roles](../04-access/01-profiles.md).

### Staying signed in

A device stays signed in until it is signed out: its session does not lapse on its own. Signing it
out under Settings › Devices, or changing the profile's password through a reset, ends it, and
Photon asks for the password again.

## Apps made for Jellyfin

photon-server answers Jellyfin's API on a port of its own, so apps made for Jellyfin, such as
Infuse, Swiftfin, Findroid and Jellyfin's own, can play from it. It is off on a new server.

1. Turn on Settings › Server › Network › Jellyfin apps. The port is 8096, Jellyfin's own, where its
   apps look first.
2. In a container, publish that port too: see [Install with Docker](02-install-docker.md#publish-other-ports).
3. In the app, add a Jellyfin server at `http://<server>:8096` and sign in with your profile's name
   and password.

An app offering Quick Connect shows six digits instead; enter them under Settings › Link a device,
as for a TV. [Use Jellyfin apps](../04-access/05-jellyfin-apps.md) covers what they can and cannot
do.

## Stremio

Stremio is not an app that plays from photon-server. It works the other way round: an admin adds a
Stremio addon to the server, whose catalogues fill collections and whose streams fill a library.
See [Stremio addons](../06-integrations/03-stremio.md).

## Scripts and other programs

A program of your own signs in as a device does, or uses an API key an admin makes. See
[API keys](../04-access/06-api-keys.md) and the [API reference](../08-reference/03-api.md).
