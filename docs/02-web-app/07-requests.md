---
title: "Ask for a title"
summary: "Ask for a film or show the server doesn't have, see what you asked for, and, as an admin, decide what people ask for and where it is sent."
---

If whoever runs the server allows it, a search also shows films and shows that none of its libraries
has yet. You can ask for one. Once someone approves it and it is added, you are told, the same way
you're told of anything new.

## Ask for a title

1. Search for it. Below what the server has, **Not in library** shows what it doesn't.
2. Choose the title. It opens as any title's page does: its picture, what it is about and who is
   in it, with **Ask for it** where **Play** would be.
3. Choose **Ask for it**.

A show is asked for whole. A title can be asked for once, whoever asks: if someone already has,
you're told so. If you only see what the server has, it doesn't take requests, or your profile has
an age limit.

## See what you asked for

Once you have asked for something, **Requests** appears in the sidebar. Each request says where it
stands:

- **Waiting**: nobody has decided yet. **Take back** withdraws it.
- **Approved**: it will be added.
- **Declined**: it won't be, with the reason if one was given.
- **Added**: it is on the server. Its name opens it.

When a request is added you get a notification, as for any new title, unless your
[notifications](06-your-settings.md) are set to nothing.

## Decide what people ask for

An admin decides any profile's requests, and a manager those of the profiles it looks after. Each
is told when a request waits, in the bell and on their browsers and devices, and can approve or
decline it from the notification. See [notifications and push](../06-integrations/06-notifications.md).

Open Settings › **Requests** to see them all. It shows those waiting first; **Show** picks another
state.

- **Approve** sends the request where its library's requests go, or leaves it for you to add the
  title yourself.
- **Decline** asks for a reason, which the person who asked sees.
- A request that **Failed** where it was sent shows why. **Approve again** sends it again.

An admin's own request is approved as it is asked.

## Let a library take requests

In Settings › Server › Libraries, open a library of files:

- **People can ask for titles from** is where a search finds titles the server lacks, such as
  TMDB. **Nothing** turns asking off for the library.
- **Requests go to** is what fetches an approved request: riven, Radarr or Sonarr, set up under
  Settings › Server › Metadata with their address and API key, or a
  [plugin](../06-integrations/01-plugins.md) that fetches titles. **Nobody** leaves approved
  requests for you to add by hand. A library is only offered what fetches every kind of title it
  holds: Radarr fetches films, and Sonarr shows.

Whatever fetches the title puts it in the library's folder, and the next scan adds it. That is when
the request is marked added, and whoever asked is told.
