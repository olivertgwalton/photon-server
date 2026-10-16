---
title: "Set up a new server"
summary: "Create the first admin, name the server, add libraries and say how it is reached from outside."
---

A new photon-server has no profiles, and nobody can sign in until someone sets it up. Setup is four
short steps in the web app: you create the admin profile, name the server, add your first
libraries, and say how the server is reached from away from home. You end signed in as the admin,
with your libraries scanning.

## Before you start

Open `http://<server>:8640` in a browser on a device on the server's own network, at the server's
own address, such as `http://192.168.1.20:8640`, or `http://localhost:8640` on the server itself.
A server not yet set up sends you to its setup page.

Setup is open only:

- **while the server has no profile.** Once the admin exists, setup never opens again; the setup
  page sends you to the sign-in page.
- **to a device on the server's local network.** That is the machine itself and the private
  networks (`192.168.0.0/16`, `10.0.0.0/8`, `172.16.0.0/12`, IPv6's `fc00::/7`, and link-local
  addresses), or the
  networks set under Settings › Server › Network › Local networks once there is someone to set
  them. Whoever reached a new server first from the internet could otherwise claim it, which is
  why Plex's claiming works the same way.
- **not through a reverse proxy.** A new server trusts no proxy, so a request through one is
  never taken as local, whatever address the proxy forwards. Set the server up at its own address,
  then add the proxy under Settings › Server › Network › Trusted proxies.

Opened from anywhere else, the page says so, and offers nothing to fill in.

## Create the admin

The first page is **Set up**, followed by the server's name.

| Field | What it does |
| --- | --- |
| Name | The admin profile's name, which you sign in with. Up to 64 characters. |
| Password | At least 8 characters. |
| Confirm password | The password again. It is asked twice because no one else can reset it yet. |

Select **Create admin**. The server makes the profile with the admin role, signs this browser in
as it, and moves on. From here on the rest of setup is an admin's pages, so each can be left and
come back to later in Settings.

From the API, ask first whether this client may set the server up:

```sh
curl http://192.168.1.20:8640/api/v1/setup
```

```json
{ "state": "open" }
```

`open` means it may; `local_only` means the server is still to be set up, from its local network;
`done` means it has been. Then create the admin, naming the device signing in:

```sh
curl -X POST http://192.168.1.20:8640/api/v1/setup \
  -H "Content-Type: application/json" \
  -d '{"name": "Alex", "password": "<password>", "device": "Laptop", "client": "curl"}'
```

The answer carries a `token` for that device, which every admin request after it sends as
`Authorization: Bearer`.

## Name the server

**Name the server** sets what apps and the sign-in page call it, and the language titles are
described in.

| Setting | What it does |
| --- | --- |
| Name | What apps and the sign-in page call this server. Until you set one, it is the host name of the machine it runs on: `photon` in the Compose file. |
| Metadata language | What titles are described in, where a library asks in no language of its own. `en-US` on a new server. |
| Certification country | Whose age ratings titles are matched with. A rating already found keeps the country it came from. The United States on a new server. |

Select **Next**. All three can be changed later under Settings › Server › General.

```sh
curl -X PUT https://photon.example/api/v1/admin/server \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Home", "metadata_language": "en-GB", "certification_country": "GB"}'
```

## Add your libraries

**Add your libraries** adds a folder of films or of shows at a time. For each:

| Field | What it does |
| --- | --- |
| Name | The library's name, as Home and the Libraries page show it. |
| Holds | Films, Shows, or Films and shows. Fixed once the library is added. |
| Its media | Files in a folder, or Streamed by a provider. A new server has no provider that streams, so it is Files in a folder. |
| Folder | A folder on the server, chosen from its own folders. Everything under it is scanned. |

The form has more settings, each with a sensible default, described in
[Add and manage libraries](../03-library/02-libraries.md). Before you add one, put your files in
the shapes the scanner reads: see [Prepare folders and name files](../03-library/01-name-your-files.md).

The folder is the server's path, not yours. In Docker it is the path inside the container, such as
`/media/Films` for the `Films` folder in `MEDIA_DIR`.

Select **Add and scan**. The library is listed above the form, its scan starts at once, and the
form is ready for the next. Select **Next** when you have added them all, or **Skip for now** to add
them later under Settings › Server › Libraries.

Titles are matched with TMDB and TheTVDB without any key of your own. To add ratings from OMDb or
MDBList, or subtitles from OpenSubtitles, add their keys afterwards under Settings › Server ›
Metadata: see [Metadata providers and artwork](../03-library/03-metadata.md).

## Watching away from home

**Watching away from home** asks two things. Both can be left for later under
Settings › Server › Network.

| Setting | What it does |
| --- | --- |
| Public address | Where people reach the server from outside, such as `https://photon.example.com`, as a TV's sign-in link names it. Left empty, each device is answered at the address it used. |
| Remote streaming limit (Mbps) | The most a stream to a device away from home is sent at, so it leaves your upload room. 0, the default, is no limit. |

Select **Next**, or **Skip for now** to leave both as they are. The rest of what reaching the
server from outside takes, port forwarding, HTTPS and a reverse proxy, is in
[Reach your server from outside](../04-access/04-remote-access.md).

## Done

The last page says the server is set up and its libraries are being scanned; titles appear as they
are matched. Select **Start watching** to go to Home.

Then:

- Add the rest of the household under Settings › Profiles: see
  [Profiles, passwords, PINs and roles](../04-access/01-profiles.md).
- Sign in from Photon and other apps: see [Connect Photon and other apps](05-connect-apps.md).
- Check that backups are made where you expect: see [Back up and restore](../07-maintain/01-backups.md).

## A forgotten admin password

Where there is no other admin to set it for you, the server resets it itself. On the sign-in page,
from a device on the server's local network, select **Forgot your password?**, give the profile's
name and select **Get a code**. The server writes a reset code to its log, which only whoever runs
the server can read:

```sh
docker compose logs server
```

The code lasts 30 minutes. Entering it with a new password and selecting **Reset password** signs
the profile out of every device. See [Profiles, passwords, PINs and roles](../04-access/01-profiles.md).
