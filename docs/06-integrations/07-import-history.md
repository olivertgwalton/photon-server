---
title: "Import watch history from Plex, Jellyfin or Emby"
summary: "Bring what a Plex, Jellyfin or Emby user has watched into a profile: watched titles, their dates, plays and where each was stopped."
---

Moving to photon-server from another server need not mean starting your watch history again. An
import signs in to the Plex, Jellyfin or Emby server you are leaving as one of its users, reads what
that user has watched and started, and writes it into one of your profiles: each title watched,
when, how many times, and where an unfinished one was stopped. Run one import for each person.

Add your libraries and let them be matched first: an import finds titles by their ids, so a title
your libraries do not hold yet, or have not matched yet, is not imported.

## Run an import

1. Open Settings › Server › Import history and choose **Import**.
2. Under **From**, choose **Plex**, **Jellyfin** or **Emby**.
3. Type the server's **Address**, such as `http://192.168.1.10:32400` for Plex or
   `http://192.168.1.10:8096` for Jellyfin and Emby.
4. Sign in as the user whose history you want:
   - **Plex**: the **Token**, the `X-Plex-Token` of that account. Plex keeps every account's
     history apart, and a server's own token is its owner's, so use the token of the account
     whose history you want.
   - **Jellyfin** and **Emby**: the user's **Username** and **Password**.
5. Choose the profile it goes **Into**, and **Start import**.

The server signs in to the other server at once, so a wrong address or credentials are caught
before anything is queued. The credentials are kept only until the import ends; a Jellyfin or Emby
session the import signed in with is signed out again at its end, so it does not linger among that
server's devices.

```sh
curl -X POST https://photon.example/api/v1/admin/imports \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"source": "jellyfin", "url": "http://192.168.1.10:8096",
       "credentials": {"username": "oliver", "password": "…"},
       "profile_id": "0199c6f2-…"}'
```

For Plex, `credentials` is `{"token": "…"}`.

### Why an import is refused

- The address is not the server's `http` or `https` address, or has a user name, password or
  query in it.
- A Plex server is given a name and password, or a Jellyfin or Emby server a token.
- The other server does not answer, or refuses the credentials.
- The profile is not one of the household's.

Each is answered `400` with the reason, and the dialog shows it.

## What an import brings

The import reads every film and episode the user has watched or started, a page at a time, and
for each it matches here:

- **Watched** titles are marked watched, as many times as the other server played them, with the
  date they were last played.
- **Started** titles keep where they were stopped, for the profile to resume from, where the
  other server says when.
- **Nothing newer is overwritten.** A title the profile has watched or played here since the
  other server last did is left as it is here, and counted as left as it is.

A title is matched by its TMDB, TheTVDB or IMDb id alone, never by name or path, so a title is
never put on the wrong film: a film by its own ids, an episode by its show's and its season and
episode numbers.

## Read how it went

Imports run one at a time on each node, as a job, **Import watch history**. Each is listed on the
page, newest first, as **Waiting to start…**, **Importing…**, then what came of it:

- **matched**: titles found here;
- **imported**: of those, written into the profile;
- **left as they are here**: of those, where the profile's own was newer, or the other server
  gave no date;
- **not found here**: titles with no ids, or none here has.

**Titles not imported** lists up to 200 of those passed over, each with why:

| Reason | What it means |
|---|---|
| No TMDB, TheTVDB or IMDb id | The other server knows the title by no id the server matches by. Fix its match there and import again. |
| Not in a library here | No title here has its ids, or an episode's show here lacks that episode. |
| No date watched | The other server says it was watched but not when, as Jellyfin does for a title marked played by hand. Where the profile has nothing of it, it is marked watched, now; where it has, it cannot be told newer than what is here, so it is left as it is and listed. |

An import the other server stopped answering ends **Failed**, with why. One cut short, by the node
stopping, runs again from the start: what it wrote is written the same again, so running it twice
does no harm. Running a whole import again later, after more titles are matched, brings what was
not found the first time.

```sh
curl https://photon.example/api/v1/admin/imports/$IMPORT_ID \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

| Route | |
|---|---|
| `GET /api/v1/admin/imports` | every import, newest first |
| `POST /api/v1/admin/imports` | start one, once the other server takes its credentials |
| `GET /api/v1/admin/imports/{id}` | how one went |
