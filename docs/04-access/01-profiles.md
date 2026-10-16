---
title: "Profiles, passwords, PINs and roles"
summary: "Add the household's profiles, decide what each may see, and know what an admin, a manager and a user may each do."
---

A profile is one person watching: their own progress, favourites, playlists and settings, and the
libraries and certificates they may see. The first profile, made as the server is
[set up](../01-get-started/04-first-run.md), is an admin. Every other is added under
Settings › Profiles.

## Roles

Every profile has one of three roles.

| Role | What it may do |
|---|---|
| Admin | Everything: the server's settings, every profile, every signed-in device, the dashboard, the activity log and API keys. |
| Manager | Adds users and keeps the ones it added: renames them, sets their passwords and pictures, chooses what they see, removes them and signs out their devices. The rest of the server is the admin's. |
| User | Watches what its access allows, and changes only its own name, password, PIN, picture and settings. |

A manager grants no more than it may see itself: a user it keeps can be given only libraries the
manager sees, and a certificate no older than the manager's. A user a manager adds starts seeing
exactly what the manager sees. Where an admin raises a user a manager added to manager or admin, it
becomes the admin's to keep. Where a manager is removed, the admin keeps its users.

The server always has an admin: the last one can be neither removed nor given another role.

## Add a profile

1. Open Settings › Profiles and choose **Add a profile**.
2. Give it a **Name**, up to 64 characters. No two profiles share a name, whatever their case.
3. Choose its **Role**. A manager is not offered the choice: it adds users.
4. Give it a **Password** of at least 8 characters. It signs in with this, and is switched to with
   it until its owner sets a PIN.
5. An admin chooses what it may see here too (see below). A manager's new profile sees what the
   manager sees, and is narrowed on its own page afterwards.

```sh
curl -X POST https://photon.example/api/v1/admin/profiles \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"name": "Sam", "role": "user", "password": "a long password"}'
```

The list under Settings › Profiles shows each profile's role, its lock (PIN or Password), and, to an
admin, when one of its devices was last seen. A manager's list holds only the users it keeps.

## Choose what a profile sees

Open the profile from Settings › Profiles. **What it sees** has three settings.

| Setting | What it does |
|---|---|
| Every library, including ones added later | On by default. Off, it sees only the libraries ticked. |
| Certificates | The oldest age rating it may watch: Any (the default), Suitable for all, or Up to 6, 7, 9, 12, 13, 15, 16, 17 or 18. |
| Titles with no certificate | Shown (the default) or Hidden, for a profile with a certificate limit. |

A title's certificate is read in the country set under Settings › Server › General, as
**Certification country**; a certificate already found keeps the country it came from.

```sh
curl -X PUT https://photon.example/api/v1/admin/profiles/$PROFILE/access \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"libraries": [], "max_age": 12, "unrated": "block"}'
```

An empty `libraries` is every library; `max_age` is an age up to 21, or `null` for any.

## Change or remove a profile

On the profile's own page, the **Profile** card renames it, changes its role, and sets a **New
password**; leave that empty to keep the password as it is. Setting a password here leaves the
profile's devices signed in: sign them out under [Devices](07-devices-and-playback.md) where that
is why you changed it.

**Remove this profile** signs out its devices, revokes any API keys it made, and forgets what it
has watched.

```sh
curl -X PATCH https://photon.example/api/v1/admin/profiles/$PROFILE \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"role": "manager"}'
```

## Passwords

A profile changes its own password under Settings › Profile, giving its **Current password** and
the new one twice. Changing it signs out every other device signed in as that profile, since a
password changed for fear of who knows it must not leave them signed in; its API keys stay. A
profile made by a [sign-in provider](02-single-sign-on.md) has no password until it sets one here,
leaving the current password empty.

Passwords are kept as argon2id hashes. Signing in is limited per address and per name, so guessing
one profile's password from many addresses is slowed too: after ten attempts, an address waits six
seconds between tries and a name six minutes.

### A forgotten password

**Forgot your password?** on the login page resets one, from a device on the server's local
networks:

1. Enter the profile's name and choose **Get a code**. The page answers the same whether or not a
   profile has that name.
2. The server writes the code to its log, on the server named on the page, where only whoever runs
   the server reads it: `docker compose logs server` in Docker.
3. Enter the code and a new password within 30 minutes. Every device on the profile is signed out.

Asking for codes and guessing them is limited to five tries, then one a minute, per address.

## PINs and switching profile

A device signed in as one profile can switch to another of the household: **Switch profile** in the
profile menu opens **Who's watching?**, which lists every profile. Switching to a profile asks for
its lock:

- A **user** with a PIN asks for its PIN, of 4 to 6 digits, which its owner sets under
  Settings › Profile. Without one, it asks for its password.
- An **admin** or a **manager** always asks for its password, so a household profile cannot come to
  keep others by guessing a few digits.

An admin cannot set another profile's PIN: a PIN is its owner's. Each device may try five switches,
then one every three minutes.

```sh
curl -X POST https://photon.example/api/v1/profiles/$PROFILE/switch \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"secret": "1234"}'
```

Every endpoint is listed in the [API reference](../08-reference/03-api.md).
