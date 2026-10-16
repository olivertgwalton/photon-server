---
title: "Notifications and push"
summary: "What each profile is told of as titles are added: in the web app's bell, on the browsers it chooses, and on its Apple devices."
---

As a scan adds films and episodes, photon-server tells each profile of the ones it cares about. A
profile reads them in the bell at the top of every page of the web app and in Photon, and can have
them pushed to its browsers and Apple devices, so a new episode is news even when nothing is open.

Each profile chooses for itself, under its own settings. An admin has nothing to set up, but the
server needs to reach the internet for anything to be pushed.

## Choose what you are told of

Open Settings › Notifications.

| Setting | What it does |
|---|---|
| Tell me of | **New episodes of shows I follow** (the default), **Every new film and episode**, or **Nothing**. A show you follow is one on your watchlist, a favourite, or one you have started. |
| From | The libraries you are told of, where the server has more than one. Clear a library's box to hear nothing of it; a library added later tells you, as the rest do. |

These are the profile's own and the same on every device it signs in on.

```sh
curl -X PATCH https://photon.example/api/v1/profile/preferences \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"notify": "all", "quiet_libraries": []}'
```

What counts as news:

- Only titles the profile may see are told.
- An episode before the furthest you have watched of its show is not news, nor is a film you have
  watched.
- A library being filled for the first time tells no one: a library that had no titles before a
  scan is being added, not added to, so a new library does not bury everyone in notifications.
- Nothing is told twice, even once it is read or cleared.

## Read them

The bell holds the newest hundred, newest first, with a count of those unread. A film is one
notification; a show's new episodes are gathered into one while it is unread, so a season added at
once is one notification that says how many it brings.

Opening a notification clears it. You can also mark every one read, or clear them all; either
leaves alone one added after the list was shown. Every device the profile is signed in on changes
with it.

```sh
curl https://photon.example/api/v1/profile/notifications \
  -H "Authorization: Bearer $TOKEN"
```

## Notify a browser

1. Open the web app at an `https` address. A browser shows notifications only to a secure page,
   and on an iPhone or iPad only to the web app added to the Home Screen.
2. Open Settings › Notifications and turn on **Notify this browser**.
3. Allow notifications when the browser asks.

Each browser is chosen on its own, and the page says how many others are notified. Where a browser
cannot be notified at that address, the page says "This browser can't be notified here"; where
notifications were blocked for the site, allow them in the browser's site settings and turn the
switch on again.

A notification shows the title's name and what it brings, in the language the browser asked in
when it was turned on. Clicking it opens the title's page and clears it. A newer notification of the
same title replaces the one shown.

### How a push is sent

The server waits a minute after a profile's last notification before it pushes, so the titles a
scan found are matched and pushed by name, and a run of scans is pushed once. It then sends each
browser a Web Push message (RFC 8030), sealed for that browser alone (RFC 8291) and signed with the
server's own key (VAPID, RFC 8292), which the server makes once and keeps in its database. The
message goes to the push service the browser named, such as Google's, Mozilla's or Apple's, which
keeps it for a day for a browser that is away: after that a new episode is no longer news to be
woken for, and the bell still has it.

So the server needs to reach those push services over HTTPS. A browser's push address must be an
`https` address on the internet; one on a private network is refused.

## Apple devices

Photon on an iPhone, iPad, Mac or Apple Vision Pro asks to be notified the same way, by its device
token. Apple delivers to an app only from a server holding that app's key, which no self-hosted
server can be given, so the server wakes the device through **photon's push relay**, at
`https://push.photon.oliverwalton.uk`.

The relay is told nothing about your libraries: each message carries the device's token and a
notification's number, and the device then asks your server for that notification and shows what
it is told. The relay's address is fixed in the server, so a device's token is never sent to an
address anyone typed. The server signs up with the relay by itself the first time it pushes, and
again whenever the relay no longer knows it.

## When a push fails

Pushing is a job, **Push notifications**, one per profile. A browser or device whose subscription
has ended, as its push service or Apple says, is forgotten. Any other failure fails the job, which
is tried again later and shows under Settings › Server › Jobs once given up on; trying again sends
every device again, and each replaces what it shows of the same title.

| Route | |
|---|---|
| `GET /api/v1/profile/notifications` | the newest hundred, and how many are unread |
| `GET /api/v1/profile/notifications/{id}` | one notification, as an Apple device shows it |
| `DELETE /api/v1/profile/notifications/{id}` | clear one, as it is opened |
| `PUT /api/v1/profile/notifications/read` | mark them read, up to the newest shown |
| `PUT /api/v1/profile/notifications/cleared` | clear them, up to the newest shown |
| `GET /api/v1/push-key` | the server's key, which a browser subscribes with |
| `GET /api/v1/profile/push-subscriptions` | the browsers and devices pushed to |
| `POST /api/v1/profile/push-subscriptions` | push to a browser, or an Apple device by its token |
| `DELETE /api/v1/profile/push-subscriptions/{id}` | stop pushing to one |
