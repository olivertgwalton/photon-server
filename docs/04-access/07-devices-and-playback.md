---
title: "Signed-in devices and what is playing now"
summary: "See and sign out the devices signed in, watch what is playing on the dashboard and stop a play, and read the activity log."
---

Every phone, browser, television and Jellyfin app that signs in holds a token of its own, so each
can be signed out without touching the rest. An admin also sees, as it happens, who is playing what
and how, and keeps a log of sign-ins, plays and changes to the server.

## Signed-in devices

Settings › Devices lists the devices signed in, the most recently seen first. Who sees which:

- a **user**, the devices signed in as itself;
- a **manager**, its own and those of the users it keeps;
- an **admin**, every device on the server.

Each shows the name the device gave itself, its app, the profile it is signed in as, and when it was
last seen, which is written at most once an hour. The device you are reading on is marked **This
browser**. API keys are not devices, and are listed under [API keys](06-api-keys.md).

**Sign out** ends a device's session at once: its next request is refused and it asks to sign in
again. Signing out **This browser** logs you out.

```sh
curl https://photon.example/api/v1/devices -H "Authorization: Bearer $TOKEN"
curl -X DELETE https://photon.example/api/v1/devices/$DEVICE -H "Authorization: Bearer $TOKEN"
```

### When a device is signed out for you

A device stays signed in through a long absence: its session ends after **90 days unused**, and
every use moves that on. Besides that, and its own sign out, a device is signed out when:

- its profile changes its password, which signs out every other device on the profile;
- its profile's password is [reset](01-profiles.md#a-forgotten-password), which signs out every
  device on it;
- its profile is removed;
- it signed in through a [sign-in provider](02-single-sign-on.md), or was paired from a device that
  did, and the account is unlinked, the provider is removed or moved, or checking back finds the
  account refused or out of its group.

An admin setting a profile's password under Settings › Profiles does not sign its devices out; sign
them out here.

## What is playing now

Settings › Server › Dashboard shows an admin **Now playing**: a card for every play on every node,
kept up to date as it goes. Each card shows:

- the title, and who is watching it, on which device and app;
- how far they are, and whether it is paused;
- how it plays: **Direct play** (the file as it is), **Direct stream** (its streams copied into
  HLS) or **Transcode** (encoded again), with the encoder where it is encoded, and the node where the
  server has more than one;
- why it is not played as it is, where it is not: "Because of: Audio codec, Bitrate";
- the file, and its video, audio and subtitles as they are, with what each becomes where the server
  encodes it, and whether the subtitles are burned in.

Beside the heading, the node answering says how many videos it is transcoding, of at most how many
where it has a limit, and how many of those are conversions for downloads. See
[Transcoding](../05-playback/01-transcoding.md) and [Playback](../05-playback/index.md) for what
each method means.

```sh
curl https://photon.example/api/v1/admin/playbacks -H "Authorization: Bearer $ADMIN_TOKEN"
```

A player tells the server where it has got to every so often; a play the server has not heard from
for two minutes is stopped, so a device that loses its connection does not hold a transcode open.

### Stop a play

**Stop this play**, on its card, stops it on that device where it is now, as Jellyfin's dashboard
stops a session: the encode ends on the node running it, and the position is kept, so the person
can resume from there.

```sh
curl -X DELETE https://photon.example/api/v1/admin/playbacks/$PLAYBACK \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Stopping a play does not sign the device out. To keep someone from starting again, sign out their
device above, or change what their profile may see under [Profiles](01-profiles.md).

## The activity log

Settings › Server › Activity lists what has happened on the server, the latest first, as it
happens. **Show** narrows it to one kind:

| Kind | `kind` |
|---|---|
| Plays started | `playback.started` |
| Plays stopped | `playback.stopped` |
| Sign-ins | `auth.signed_in` |
| Refused sign-ins | `auth.sign_in_refused` |
| Profiles added | `profile.added` |
| Profiles removed | `profile.removed` |
| Libraries added | `library.added` |
| Libraries removed | `library.removed` |
| Scans | `library.scanned` |
| Titles added | `library.titles_added` |
| Failed tasks | `task.failed` |
| Backups | `backup.made` |
| Dead jobs | `job.dead` |

A sign-in, or a refused one, records the name tried, the device, the app and the address it came
from, as Jellyfin logs one; never the password. A sign-in through a provider records the profile it
signed in as, and a refused one the account the provider named, where it got that far.

Entries are kept for 30 days: the scheduled task **Prune the activity log** forgets older ones (see
[Scheduled tasks](../07-maintain/04-tasks-and-jobs.md)).

```sh
curl "https://photon.example/api/v1/admin/activity?kind=auth.sign_in_refused" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

To be told of these as they happen elsewhere, see [Webhooks](../06-integrations/05-webhooks.md).
