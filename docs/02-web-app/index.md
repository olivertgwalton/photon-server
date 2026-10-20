---
title: "Use the web app"
summary: "What photon-server's own web app does for someone watching, and where each part of it is."
---

photon-server comes with its own web app. Open the server's address in a browser, such as
`https://photon.example`, and log in. From there you can browse your libraries, search, keep a
watchlist, favourites and playlists, watch in the browser, and choose how things play.

This section is for someone watching. Running the server is covered in the sections after it.

## Logging in

![The log-in page, asking for a name and password](../images/web-app-login.webp)

1. Type your profile's **Name** and **Password**.
2. Choose **Log in**. You will see **Who's watching?** if your household has more than one
   profile.
3. Choose your profile. Another person's profile asks for its PIN or password first.

If your household uses a shared sign-in, such as a company or family account, the page also offers
**Continue with** that service. See [single sign-on](../04-access/02-single-sign-on.md).

To change profile later, choose your picture at the top right, then **Switch profile**.

### If you forget your password

1. Choose **Forgot your password?**. You will see **Reset your password**.
2. Type your **Name** and choose **Get a code**. You must do this from a device on the same home
   network as the server.
3. The code is not emailed. It is written to the server's log, so ask whoever runs the server to
   read it to you.
4. Type the **Code**, your **New password** and **Confirm password**, then choose **Reset
   password**.

The code only works for a short time. Resetting your password signs you out on every device.

## Finding your way around

![Home, with the sidebar on the left and search at the top](../images/web-app-home.webp)

The sidebar on the left holds:

- **Home** and **Calendar**.
- **Libraries**: each library you have been given, then **Collections** if the server has any.
- **Watchlist**, **Favourites**, **Playlists**, **History** and **Downloads**.
- **Plugins**, if a plugin adds pages of its own. See [plugins](../06-integrations/01-plugins.md).
- **Settings**, at the bottom.

Along the top of every page:

- The search field. See [search](02-search.md).
- The bell, which holds your [notifications](06-your-settings.md#notifications).
- Your picture, which opens **Switch profile**, **Settings**, **Link a device** and **Log out**.

An admin also sees a **Server dashboard** button and the server's activity. Those are covered in
the admin sections.

You only ever see the libraries and titles an admin has given your profile. If the same title is in
two of your libraries, Home, search and the calendar show it once.

The web app keeps itself up to date. If a title is added, or you mark something watched on another
device, the page changes by itself. There is no need to refresh.

## Pages

- [Home and browsing](01-home-and-browsing.md): Home, a library, filters and sorting, a title's
  page and a person's.
- [Search](02-search.md): finding a title or a person.
- [Your lists](03-your-lists.md): the watchlist, favourites, history and the calendar.
- [Playlists](04-playlists.md): making, ordering and playing playlists, by hand or by filters.
- [Watching](05-watching.md): the player, subtitles, skipping, casting and downloads.
- [Your settings](06-your-settings.md): your profile, playback, Home's rows, notifications,
  devices, linking a TV, sign-in and trackers.
- [Ask for a title](07-requests.md): asking for what the server doesn't have, and deciding what
  people ask for.
