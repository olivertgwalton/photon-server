---
title: "Sign in on a TV"
summary: "Pair a television, or any device without a keyboard, by the code it shows, approved from a device already signed in."
---

Typing a password with a remote is slow, so a television signs in by pairing instead, as RFC 8628
describes: it shows a short code, you enter that code on a phone or computer already signed in, and
the television is signed in as your profile with a token of its own. Photon on Apple TV pairs this
way, and Jellyfin apps' Quick Connect is the same pairing (see [Use Jellyfin apps](05-jellyfin-apps.md#quick-connect)).

## Pair a television

1. On the television, choose to sign in with a code. It shows a code such as `BCDF-GHJK`, and the
   address to enter it at, which may also be a QR code.
2. On a phone or computer signed in as the profile the television should watch as, open
   Settings › Link a device, or the address the television shows, or scan its QR code, which fills
   the code in.
3. Enter the code and choose **Link**. The page says which device was signed in, and the television
   notices within a few seconds.

The television signs in as the profile that approved it: to pair it as someone else, approve it from
a device signed in as them, or [switch profile](01-profiles.md#pins-and-switching-profile) on the
television afterwards.

A paired television's session is tied to the approving device's sign-in where that came from a
[sign-in provider](02-single-sign-on.md): unlinking the account, or the provider signing it out,
signs the television out too.

## The code

- A code is eight letters, written `XXXX-XXXX`, from twenty consonants chosen so none is mistaken for another
  when read off a screen, and has no vowels, so it never spells a word. Case, dashes and spaces are
  ignored as you type it.
- It lasts **10 minutes**. A code not approved by then is gone, and the television asks for another.
- It is approved once, and the television receives its token once.

Starting pairings is limited to ten, then one every six seconds, per address, and approving them to
five, then one every twelve seconds, per profile, so codes cannot be guessed by trying them.

## The address the television shows

The television shows `/link` at the server's **Public address**, set under Settings › Server ›
Network, so a phone away from your home network reaches it too. Where none is set, it shows the
address the television itself used. `/link` opens Settings › Link a device. See
[Reach your server from outside](04-remote-access.md).

## Signing a television out

A paired television is listed under Settings › Devices with every other device, and is signed out
there. It stays signed in through long absences: a device's session lapses only after 90 days
unused. See [Signed-in devices](07-devices-and-playback.md).

## For app developers

Pairing is three calls. The television starts a pairing, naming itself:

```sh
curl -X POST https://photon.example/api/v1/auth/pairings \
  -H "Content-Type: application/json" \
  -d '{"device": "Living room", "client": "My TV app"}'
```

```json
{
  "device_code": "…",
  "user_code": "BCDF-GHJK",
  "verification_uri": "https://photon.example/link",
  "verification_uri_complete": "https://photon.example/link?code=BCDF-GHJK",
  "poll_interval_ms": 5000,
  "expires_in_ms": 600000
}
```

A signed-in device approves it:

```sh
curl -X POST https://photon.example/api/v1/auth/pairings/approvals \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"user_code": "BCDF-GHJK"}'
```

Meanwhile the television asks every `poll_interval_ms` whether it is approved, signing in by the
pairing:

```sh
curl -X POST https://photon.example/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"method": "pairing", "device_code": "…"}'
```

Until it is approved the answer is a 400 problem whose code is RFC 8628's own:
`authorization_pending` while it waits, `slow_down` for asking sooner than the interval, and
`expired_token` once the code has lapsed. Approved, it answers the token and the profile, once.
