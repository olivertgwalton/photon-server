---
title: "Webhooks"
summary: "Tell another service when something happens on the server, as signed JSON or as a message to Discord, Slack, ntfy or Gotify."
---

A webhook is an address the server posts to when something happens: a play, a sign-in, titles
added, a task that failed. Use one to feed an automation of your own, as signed JSON, or to send a
message to a chat or push service. By the end of this page you have a webhook added, tested, and,
for JSON, checked by its signature.

## Add a webhook

1. Open Settings › Server › Webhooks and choose **Add a webhook**.
2. Type the **Address** to post to. For a chat or push service, use the address it gives you,
   with its token in it.
3. Choose how it is **Sent as**: **Photon JSON, signed**, **Discord**, **Slack**, **ntfy** or
   **Gotify**.
4. Tick the events under **Send it when**, and choose **Add webhook**.
5. For Photon JSON, the server shows the webhook's secret once. Copy it to the service that
   receives the webhook: it is not shown again.

```sh
curl -X POST https://photon.example/api/v1/admin/webhooks \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"url": "https://automation.example/photon", "events": ["playback.started", "library.titles_added"]}'
```

The answer carries the `secret`, this once. `format` is `photon` where it is left out, or one of
`discord`, `slack`, `ntfy` and `gotify`. The address must be `http` or `https`, and each event may
be named once.

### The events

| Send it when | Event |
|---|---|
| A play starts | `playback.started` |
| A play is paused | `playback.paused` |
| A play is resumed | `playback.resumed` |
| A play stops | `playback.stopped` |
| Someone signs in | `auth.signed_in` |
| A sign-in is refused | `auth.sign_in_refused` |
| A profile is added | `profile.added` |
| A profile is removed | `profile.removed` |
| A library is added | `library.added` |
| A library is removed | `library.removed` |
| A library is scanned | `library.scanned` |
| Titles are added | `library.titles_added` |
| A task fails | `task.failed` |
| A backup is made | `backup.made` |

These are what happens to people and libraries, as Plex's webhooks tell plays and new titles. The
server's own work (jobs started and finished, scan progress) is not offered: a webhook is sent by
a job, so one told of jobs would set off another without end.

## What is sent

### Photon JSON

Each event is a `POST` of a JSON body, with these headers:

| Header | |
|---|---|
| `Content-Type` | `application/json` |
| `User-Agent` | `photon-server` |
| `X-Photon-Event` | the event, such as `playback.started` |
| `X-Photon-Signature` | `sha256=` and the hex HMAC-SHA256 of the body, keyed with the webhook's secret |

```json
{
  "event": "playback.started",
  "at": "2026-10-09T20:15:00Z",
  "server": {"id": "0199c6f2-…", "name": "Den"},
  "profile": {"id": "0199c6f2-…", "name": "Oliver"},
  "title": {"id": "0199c6f2-…", "kind": "movie", "title": "Heat", "year": 1995, "ids": {"imdb": "tt0113277"}},
  "details": {"playback": {"method": "direct", "position_ms": 0}},
  "text": "Oliver started Heat"
}
```

- `server` is the server's id and the name it goes by under Settings › Server › General.
- `profile`, `title` and `library` are there where the event is about one, and it still exists
  when the event is sent.
- A `title` carries its IMDb, TMDB and TheTVDB `ids`, as a scrobbler finds it by. An episode carries
  its `season` and `episode`, and its `show`, with the show's own ids.
- `details` are the event's own. A play's are the playback as the dashboard shows it (the device,
  the copy, `method` as `direct`, `remux` or `transcode`, and `position_ms`), and for a stop its
  `reach` (`start`, `resumable` or `end`, the last meaning watched) and `stopped_by` (`player`,
  `admin`, or `sweep` for a player that stopped reporting). A sign-in's are the `name`, `address`,
  `device` and `client`, never a password; a scan's its `folders` and how many files it read; a
  failed task's its `task` and `error`; a backup's its `file`.
- `text` is what happened as a sentence, in English, as Settings › Server › Activity says it.

### Check the signature

Compute the HMAC of the body exactly as it arrived, before parsing it, and compare it with the
header. In a shell, with the body in `body.json`:

```sh
printf 'sha256=%s\n' "$(openssl dgst -sha256 -hmac "$SECRET" -hex < body.json | awk '{print $NF}')"
```

A body whose signature does not match was not sent by your server, or was changed on the way.

### Discord, Slack, ntfy and Gotify

For these, the server sends the server's name over the event's `text`, as each service takes a
message:

| Sent as | Body |
|---|---|
| Discord | `{"username": "<server>", "content": "<text>"}` |
| Slack | `{"text": "<server>: <text>"}` |
| ntfy | the text, as plain text, with the server's name as its `Title` header |
| Gotify | `{"title": "<server>", "message": "<text>"}` |

No secret is shown for these, as the services check none: the token in the address is what lets
the server post there.

## Test a webhook

Choose **Send a test** on a webhook's row. The server queues a `webhook.test` event to that webhook
alone, whatever events it asked for.

```sh
curl -X POST https://photon.example/api/v1/admin/webhooks/$WEBHOOK_ID/test \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

The answer is `202 Accepted`: the test is queued, not yet sent. Whether it arrived is the receiver's
to say, or the Jobs page's when it did not.

## When a delivery fails

Each event is queued as a job, **Send a webhook**, as it happens, so a slow or absent receiver never
holds up the play or scan that raised it. Each node sends two at a time.

The receiver has 10 seconds to answer. Anything but a `2xx` fails the delivery, and so does a
redirect, which the server does not follow, as following it would turn the `POST` into a `GET` with
no body. A failed delivery is tried again later, waiting twice as long each time, five times in
all; then it is given up on and listed under Settings › Server › Jobs › **Given up**, where it can
be tried again. The error names the receiver's host and its answer, never the rest of the address,
which may carry the receiver's token.

## Remove a webhook

Choose **Remove** on its row and confirm. The webhook is told nothing more, and whatever was still
waiting to be sent to it is dropped.

A plugin that hears events is sent them in the same way, as a webhook of its own. See
[Write a plugin](02-write-a-plugin.md).

| Route | |
|---|---|
| `GET /api/v1/admin/webhooks` | the webhooks, without their secrets |
| `POST /api/v1/admin/webhooks` | add one, answering its secret once |
| `POST /api/v1/admin/webhooks/{id}/test` | send it a `webhook.test` |
| `DELETE /api/v1/admin/webhooks/{id}` | remove it and what waits to be sent to it |
