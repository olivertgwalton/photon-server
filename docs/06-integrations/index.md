---
title: "Integrations"
summary: "Connect photon-server to plugins, Stremio addons, trackers, webhooks, notifications and the server you are moving from."
---

photon-server talks to other services in both directions. Plugins and Stremio addons tell it about
titles, keep lists of them and stream them; trackers hear what each profile watches; webhooks tell
other services what happens on the server; notifications tell each profile what has been added;
and an import brings the watch history from the Plex, Jellyfin or Emby server you are leaving.

Each of these is set up by an admin under Settings › Server, and the parts that belong to one
profile, such as a tracker account or a browser that is notified, are set by that profile under
its own settings.

## Pages

- [Install and manage plugins](01-plugins.md): register a plugin, set it up, give it a place in a
  library's sources, and remove it.
- [Write a plugin](02-write-a-plugin.md): the protocol a plugin speaks, for its author.
- [Stremio addons](03-stremio.md): register an addon, make collections of its catalogs, and play
  what it streams in a remote library.
- [Sync with trackers](04-trackers.md): Trakt, Simkl, MDBList and AniList, set up by an admin and
  linked by each profile.
- [Webhooks](05-webhooks.md): send events to another service as signed JSON, or as a message to
  Discord, Slack, ntfy or Gotify.
- [Notifications and push](06-notifications.md): what each profile is told of as titles are added,
  in the web app and on its browsers and Apple devices.
- [Import watch history](07-import-history.md): bring what a Plex, Jellyfin or Emby user has
  watched into a profile.
