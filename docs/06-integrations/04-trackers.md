---
title: "Sync with trackers"
summary: "Tell Trakt, Simkl, MDBList and AniList what each profile watches, through an app you register on each."
---

A tracker is a service that keeps what someone watches across every app they use. photon-server
speaks to four: **Trakt**, **Simkl**, **MDBList** and **AniList**. An admin registers an app on
each tracker the household uses and gives the server its client id; each profile then links its
own account, and from then on the server tells that account what the profile plays and marks
watched.

The server tells trackers; it does not read them. What a profile watched before it linked an
account stays where it is: to bring history in from another server, see
[Import watch history](07-import-history.md).

## Set up a tracker

Each tracker signs accounts in through an app registered on it, and the server needs only that
app's client id. Open Settings › Server › Trackers, follow the link on the tracker's card, and
register the app as the card says:

| Tracker | What to register |
|---|---|
| Trakt | An app at [Trakt's apps](https://app.trakt.tv/settings/apps) with the redirect URI `urn:ietf:wg:oauth:2.0:oob`. Trakt asks for one, and refreshes an account's access only with it. |
| Simkl | An **AUTH V2** app at [Simkl's developer settings](https://simkl.com/settings/developer/), of the type "TV, devices & command line". |
| MDBList | An app at [MDBList's developer page](https://mdblist.com/developer/), of the type "Device Code App". |
| AniList | A client at [AniList's developer settings](https://anilist.co/settings/developer) with the redirect URL `https://anilist.co/api/v2/oauth/pin`, which shows each profile its token to paste. Copy its **ID**. |

Paste the client id under **Client id** and choose **Save**. The card then says **Set up**.

```sh
curl -X PUT https://photon.example/api/v1/admin/trackers/trakt \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"client_id": "…"}'
```

No client secret is asked for: Trakt, Simkl and MDBList sign a device in by a code (RFC 8628's
device flow), which needs the client id alone, and AniList shows its token on its own page.

MDBList as a tracker is apart from MDBList as a metadata provider: the provider, under Settings ›
Server › Metadata, takes an API key for ratings and lists, and the tracker takes an app's client id
for each profile's account.

### Take a tracker away

Empty the **Client id** and save. No profile can link that tracker from then on. Accounts already
linked stay linked, but the server tells them nothing until a client id is set again.

## Link your account

Each profile links its own account, under Settings › Trackers. A tracker no admin has set up says
so there, and an admin sees a link to Settings › Server › Trackers.

### Trakt, Simkl and MDBList

1. Choose **Link Trakt**, **Link Simkl** or **Link MDBList**.
2. The card shows a code and the address to enter it at, and how long it has left. Choose
   **Open Trakt** (or the tracker's name) to open its page with the code filled in, or enter the
   code there yourself, and allow the server.
3. The card changes to **Linked as** your name on the tracker, on every device the profile is
   signed in on, as soon as the tracker says the code was entered.

The server asks the tracker after the code as often as the tracker allows, slowing down when it is
asked to. **Cancel** stops linking. A code that expires or is refused ends the link, and the card
offers to link again.

```sh
curl -X POST https://photon.example/api/v1/profile/trackers/trakt/link \
  -H "Authorization: Bearer $TOKEN"
```

A tracker that does not know the client id refuses the link: "… refused the client id: an admin
checks it is the id of an app registered there". A profile that has linked an account on a tracker
already is refused until it unlinks it.

### AniList

AniList has no device flow, so linking it is a token you bring back.

1. Choose **Link AniList**, then **Open AniList** to sign in there and allow the server.
2. AniList shows a token. Paste it into the card and choose **Link**.

The card waits 15 minutes for the token. AniList's token lasts a year and is never refreshed: when
it ends, the account is unlinked and you link it again.

```sh
curl -X PUT https://photon.example/api/v1/profile/trackers/anilist/token \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"token": "…"}'
```

### Unlink

Choose **Unlink** and confirm. The server forgets the account and asks the tracker to forget what
it granted the server. What the tracker already keeps stays there. AniList's grants are revoked on
AniList's own site alone, so revoke the app there too.

## What the server tells a tracker

**Plays.** As a profile's play starts, pauses, resumes and stops, the server tells each tracker it
linked, with how far through the title the player is. Trakt, Simkl and MDBList count a title
watched from 80% through, and keep where a play stopped short of that. A play the server itself
counts watched is told as complete, whatever share of it that was, as a film's credits may begin
before a tracker's 80%. AniList keeps no plays and is told only what was watched.

**Watched marks.** Marking a title watched, or unwatched, is pushed to each linked account as
well: the server waits two minutes, so the play that marked it is told first and nothing is counted
twice, then sends what changed. Removals go before additions, so a push cut short and sent again
never adds a watch twice. A change made before the account was linked is not pushed.

AniList is told how far into each entry the account is: the server moves an entry on to the
furthest episode watched, and completes it at its last, but never moves one back, so marking an
episode unwatched leaves AniList as it is. It finds an anime's entry for an episode through the
published anime lists, which the scheduled task **Refresh the anime lists** reads again.

**How a title is found.** A film is told by its TMDB, TheTVDB and IMDb ids, and an episode by its
show's ids and its season and episode numbers, which every tracker matches. A title the server knows
by none of those ids, or an episode with no number, is not told.

### Tokens

The server refreshes an account's access a day before it expires, and once more if a tracker
refuses it early. A tracker that refuses a refresh for good, because its user revoked the app or the
grant lapsed unused, unlinks the account, and the profile's Trackers page offers to link it again.

### When a tracker is slow

Plays are told one at a time by the node the play is on. That node holds up to 256 waiting to be
told; past that it drops the newest and logs "a play was not told to trackers: too many are
waiting". A watched mark that could not be pushed is tried again by any node five minutes later.
Failures are logged with the tracker's name: "could not tell a tracker of a play", "could not tell
a tracker what was watched".

| Route | |
|---|---|
| `GET /api/v1/admin/trackers` | each tracker and its client id |
| `PUT /api/v1/admin/trackers/{tracker}` | set a tracker's client id, or empty it |
| `GET /api/v1/profile/trackers` | the profile's account on each, or the code it is entering |
| `POST /api/v1/profile/trackers/{tracker}/link` | ask for a code, or AniList's page |
| `PUT /api/v1/profile/trackers/{tracker}/token` | link AniList by its token |
| `DELETE /api/v1/profile/trackers/{tracker}` | unlink, or stop linking |
