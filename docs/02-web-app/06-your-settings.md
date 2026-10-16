---
title: "Your settings"
summary: "Your profile, how you play, Home's rows, the devices signed in as you, linking a TV, signing in with another account, and trackers."
---

**Settings**, at the foot of the sidebar, opens on **Your account**: the pages every profile has.
An admin sees **Server** under them, and a manager **Profiles**; those are covered in
[giving people access](../04-access/index.md) and the sections after it. **Search settings**, at
the top of the list, finds a page or a single setting by name and opens the page at it.

Almost everything here is your profile's, on every device and in every app signed in as you. The
exception is how subtitles look, which is each browser's own.

## Profile

The page heads with your picture, name and role. **Add a picture** (or **Change picture**) takes a
JPEG, PNG, GIF or WebP of at most 32 MiB; **Remove picture** takes it away.

| Setting | What it does |
| --- | --- |
| Name | What every device calls you, up to 64 characters. No two profiles share a name, and the new one shows on every device at once. |
| Password | **Current password**, **New password** and **Repeat the new password**. Changing it signs out every other device signed in as you. A profile made by logging in with a sign-in provider has no password until you set one here, leaving **Current password** empty. |
| PIN | 4 to 6 digits, asked for in place of your password when someone switches to your profile on **Who's watching?**. **Remove PIN** goes back to the password. An admin's profile has no PIN: it is always opened with its password. |

What your profile may see (its libraries, and any limit on what it is shown) is not here: an admin
or a manager sets it, under [profiles](../04-access/01-profiles.md).

```sh
curl -X PATCH https://photon.example/api/v1/profile \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name": "Ada"}'
```

## Playback

How you play, wherever you sign in. The server chooses the sound and subtitles of a title by these,
and the Photon apps and the web app's player follow the rest. A track or quality chosen in the
player still wins for that title. Each change is saved as you make it.

| Setting | What it does |
| --- | --- |
| Quality | The most the server sends. **Original: the file as it is** by default, or **Up to** a ceiling from 40 Mbps down to 420 kbps. Above it, the server converts the title down. Lower it on a slow connection. |
| Audio language | The language you want to hear. **Any** by default. |
| Play the default track | On by default: the sound the file marks as its default, whatever its language. Off, the sound in your **Audio language** plays where there is one. |
| Subtitles | When subtitles come on without being asked for. **As the file says** by default: a track the file marks default or forced. **When the sound is in another language**: a full track in your subtitle language when the sound is not in it, else forced subtitles alone. **Always**: a full track in your subtitle language, else a forced one. **Only forced subtitles**: only the lines in another language translated. **Never**. |
| Subtitle language | The language subtitles are chosen in. **Any** by default. |
| Remember the sound chosen | On by default: a title plays again with the sound you last chose for it. |
| Remember the subtitles chosen | On by default: a title plays again with the subtitles you last chose for it, or none. |
| Play the next episode | On by default: as the credits roll, the next episode plays after a ten-second count. Off, it is offered and waits. |
| Intros and recaps | **Show a button to skip** by default, **Skip by itself**, or **Don't offer to skip**. |
| Credits and previews | The same three. Skipping the credits at the end of an episode goes on to the next. |
| Play theme music | Off by default. On, a film's or show's theme plays quietly, once, while you look at its page, with a button to stop it. |

Skipping works only where the server has found where the intro or credits are; see
[markers and previews](../03-library/09-markers-and-previews.md). Theme music is there only for
titles the server has a tune for; see [theme music](../03-library/11-theme-music.md).

```sh
curl -X PATCH https://photon.example/api/v1/profile/preferences \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"max_bitrate_kbps": 8000, "subtitle_mode": "smart", "subtitle_language": "en"}'
```

What is left out of the request stays as it was. `GET` on the same address reads them all.

### Subtitles on this device

How plain text subtitles look in this browser, kept by the browser rather than the server, since it
depends on the screen and how far you sit from it. Styled subtitles keep the look their file gives
them, and picture subtitles are shown as they were made.

| Setting | What it does |
| --- | --- |
| Subtitle size | **Small**, **Normal** (the default), **Large** or **Larger**. |
| Subtitle colour | **White** (the default) or **Yellow**. |
| Subtitle background | **Dark box** (the default) or **Shadow**. |

## Home rows

**Home** lists every row your Home can have, in the order you see them. Drag a row by its grip, or
move it with its arrows; the switch beside it shows or hides it. A hidden row is not drawn on any
device. The rows are described in [Home](01-home-and-browsing.md#home).

A library's rows (Recently Added, Recently Released and Top Rated) are one entry each here, and
drawn once for each library they suit, in the order you put your libraries in the sidebar.

```sh
curl -X PATCH https://photon.example/api/v1/profile/preferences \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"home": [{"row": "next_up", "visibility": "shown"}, {"row": "continue_watching", "visibility": "shown"}, {"row": "top_rated_unwatched", "visibility": "hidden"}]}'
```

The rows named come first, in that order; any left out follow, shown, in their usual order. A row
the server gains in a later version is added at the foot, shown.

## Sign-in

Where an admin has set up a sign-in provider, such as your household's OpenID Connect server, this
page offers **Link** for each. Linking sends you to the provider to sign in and back here; from
then on **Continue with** that provider on the log-in page logs you in as this profile. **Unlink**
stops that, and signs out the devices you signed in with it, or paired from one: set a password
first if you have none. Setting providers up is in [single sign-on](../04-access/02-single-sign-on.md).

## Devices

**Devices** lists what is signed in to your profile, **This browser** among them, with the app and
when each was last seen. **Sign out** ends a device's session at once; signing this browser out
logs you out. A manager also sees the devices of the profiles they keep, and an admin every
device; see [signed-in devices](../04-access/07-devices-and-playback.md).

## Link a device

A television, or any app that signs in with a code, shows a code such as `ABCD-EFGH`. Sign it in
as you without typing a password on it:

1. On the TV, choose to sign in with a code.
2. In the web app, open **Link a device** (in Settings, or from your picture's menu). The address
   the TV shows, ending `/link`, opens the same page, with the code filled in where the TV put it
   in the address.
3. Type the **Code** and choose **Link**.

The page says the device is signed in; the TV may take a few seconds to notice. The device is then
yours, listed under **Devices**.

```sh
curl -X POST https://photon.example/api/v1/auth/pairings/approvals \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"user_code": "ABCD-EFGH"}'
```

How pairing works, and its limits, are in [signing in on a TV](../04-access/03-pair-a-tv.md).

## Trackers

**Trackers** links your accounts on **Trakt**, **Simkl** and **MDBList**, which keep what you watch
across apps. Once linked, the server tells each tracker what you play as you play it, and what you
mark watched or unwatched.

1. Choose **Link** under the tracker. A tracker an admin has not set up yet says so instead.
2. The page shows a code and the tracker's address. **Open** the tracker, sign in there and allow
   the server, or enter the code at that address before it expires.
3. The page reloads as **Linked as** your name on the tracker.

**Unlink** has the tracker forget this server's access; what it already keeps stays there. Setting
trackers up, and what each is told, are in [sync with trackers](../06-integrations/04-trackers.md).
