---
title: "API keys"
summary: "Make a token for a script or another server, use it, and revoke it."
---

An API key lets a script or another program, such as Sonarr or a home automation, reach the server
without signing in as a device. Only an admin makes, lists and revokes them, under
Settings › Server › API keys.

## What a key is

- A key **acts as the admin who made it**, with whatever role that profile has when the key is used:
  where the profile stops being an admin, its keys can do only what it may do now.
- A key **never lapses** on its own, unlike a device, whose session ends after 90 days unused. It
  works until it is revoked, or until the profile that made it is removed.
- **Changing the password leaves keys working**, as Jellyfin's do: a key is not the password, and
  revoking one is its own act.
- The server keeps **only a hash** of a key, so the key itself is shown once, when it is made.

A key, like a device's token, starts `pst_`, so a secret scanner can find one that leaked.

## Make a key

1. Open Settings › Server › API keys and choose **Make a key**.
2. Say **What it is for**, up to 64 characters, such as `Sonarr`.
3. Choose **Make key**, and copy the key it shows. It is not shown again.

```sh
curl -X POST https://photon.example/api/v1/admin/keys \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"name": "Sonarr"}'
```

```json
{"id": "…", "token": "pst_…"}
```

To make the first key with `curl` you need an admin's token. Signing in as a device answers one:

```sh
curl -X POST https://photon.example/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"method": "password", "name": "Alex", "password": "…", "device": "Laptop", "client": "curl"}'
```

That token is a device's, listed under Settings › Devices, and lapses after 90 days unused; a key
does not.

## Use a key

Send it as a bearer token in the `Authorization` header, on every request:

```sh
curl https://photon.example/api/v1/admin/libraries -H "Authorization: Bearer $ADMIN_TOKEN"
```

photon's API never reads a token from the query string, where it would land in logs. Every route and
who may call it is in the [API reference](../08-reference/03-api.md).

## List and revoke keys

Settings › Server › API keys lists every key: what it is for, the admin who made it, when, and when
it was last used. Last used is written at most once an hour, so it is right to within the hour.

**Revoke** a key, and whatever uses it is refused from then on.

```sh
curl -X DELETE https://photon.example/api/v1/admin/keys/$KEY \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```
