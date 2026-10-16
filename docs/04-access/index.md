---
title: "Give people access"
summary: "Who signs in to photon-server, how, from where, and how an admin sees and ends what they are doing."
---

Everyone who watches has a **profile**: their own progress, favourites and playlists, and what they
may see. A device signs in as a profile by its name and password, through a sign-in provider your
household already uses, or, on a television, by a code another device approves. Each signed-in
device holds a token of its own, which an admin, or the profile itself, can sign out at any time.

This section covers who may do what on the server, the ways in, reaching the server from outside
your home, and the Jellyfin apps that can use it too.

## Pages

- [Profiles, passwords, PINs and roles](01-profiles.md): add profiles, choose what each may see,
  and what an admin, a manager and a user may each do.
- [Set up single sign-on](02-single-sign-on.md): sign in through an OpenID Connect provider such as
  Authelia, Authentik, Keycloak or Pocket ID.
- [Sign in on a TV](03-pair-a-tv.md): pair a television by the code it shows.
- [Reach your server from outside](04-remote-access.md): HTTPS, certificates, the public address,
  reverse proxies, local networks and the remote bitrate limit.
- [Use Jellyfin apps](05-jellyfin-apps.md): let Infuse, Swiftfin, Findroid and Jellyfin's own apps
  sign in.
- [API keys](06-api-keys.md): tokens for scripts and other servers.
- [Signed-in devices and what is playing now](07-devices-and-playback.md): sign devices out, watch
  the dashboard's Now playing, stop a play, and read the activity log.
