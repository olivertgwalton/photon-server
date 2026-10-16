---
title: "Integrations"
summary: "Connect photon-server to plugins, Stremio addons, trackers, webhooks, notifications and the server you are moving from."
---

photon-server can work with other services and apps:

- **Plugins and Stremio addons** add information about titles, lists of titles, and things to
  stream.
- **Trackers** such as Trakt keep a record of what each person watches.
- **Webhooks** tell another service, such as Discord, when something happens on the server.
- **Notifications** tell each person when new episodes and films arrive.
- **Importing** brings watch history over from Plex, Jellyfin or Emby.

An admin sets each of these up under Settings › Server. The parts that belong to one person, such
as their Trakt account or which browsers notify them, they set under their own settings.

## Pages

- [Install and manage plugins](01-plugins.md): add a plugin, set it up, and remove it.
- [Stremio addons](03-stremio.md): add an addon, make collections from its lists, and play what it
  streams.
- [Sync with trackers](04-trackers.md): Trakt, Simkl, MDBList and AniList.
- [Webhooks](05-webhooks.md): send what happens on the server to Discord, Slack, ntfy, Gotify or
  your own automation.
- [Notifications and push](06-notifications.md): be told about new titles in the web app, in your
  browser and on your Apple devices.
- [Import watch history](07-import-history.md): bring what someone watched on Plex, Jellyfin or
  Emby.

If you are a developer and want to build a plugin of your own, see
[Write a plugin](../08-reference/06-write-a-plugin.md).
