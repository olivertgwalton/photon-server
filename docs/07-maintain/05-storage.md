---
title: "Storage"
summary: "Where photon-server keeps artwork, avatars, theme tunes and previews, on each server's disk or in an S3-compatible bucket, and how to move them."
---

Besides its database, photon-server keeps files of its own: the artwork it fetches and you upload, profiles' avatars, theme tunes, and the chapter images and seek previews it makes. Settings › Server › Storage chooses where. A server on its own keeps them on its own disk; several servers can share them in a bucket. This page covers both, the cache folder around them, and moving from one to the other.

## The cache folder

Every server has a cache folder, `photon-server` in the system's cache folder: `/var/cache/photon-server` in the Docker image (the compose file keeps it in the `cache` volume), `~/.cache/photon-server` on Linux, `~/Library/Caches/photon-server` on a Mac and `%LOCALAPPDATA%\photon-server` on Windows. Settings › Server › Dashboard shows it as **Cache**, with the space free.

It holds:

- `artwork` and `previews`, where artwork, avatars, theme tunes and previews are kept on disk.
- `hls`, the segments of the streams the server is making now, about a minute behind each player and up to a minute ahead. A new transcode needs 2 GiB free here. The folder is emptied as the server starts.
- `downloads`, the conversions made for [downloads](../05-playback/03-downloads.md) while one needs them.
- `subtitles` and `fetched-subtitles`, subtitles read out of files, and copies of those fetched from providers, which the database keeps, forgotten once no one has read them lately.
- `node`, the server's id among the nodes. A server given an empty cache folder joins as a new node.

Only `artwork` and `previews` move to a bucket; the rest stays on each server's disk, as it is that server's own work.

## Each server's own disk

**Kept on: Each server's own disk** is the default. Each server keeps what it fetches, uploads and makes in its own cache folder. With several servers, each fetches every picture itself and makes its own previews, and an avatar uploaded through one is kept on that one; a bucket shares them.

Pictures uploaded and avatars are only here, not in the database's [backups](01-backups.md): back the folder's `artwork` up with the machine.

## An S3-compatible bucket

**Kept on: An S3-compatible bucket** keeps artwork and previews in one bucket every server shares: Amazon S3, or any store that speaks its API.

1. Make a bucket, and credentials that may put, read, list and delete objects in it.
2. Open Settings › Server › Storage and set **Kept on** to An S3-compatible bucket.
3. Fill in the bucket, and choose **Check**. The server puts a small object in the bucket, reads it back, lists it and removes it, and says what failed if any of that does.
4. Choose **Save**. Saving checks the bucket again first, and is refused if the check fails.

| Setting | What it does |
|---|---|
| Address | The store's address, such as `https://s3.example.com` or `http://minio:9000`. Empty for Amazon S3. |
| Name | The bucket's name. Required. |
| Folder | Where in the bucket to keep things. Empty for its root. |
| Region | The bucket's region. Empty to ask the store. |
| Access key, Secret key | What signs each request: both or neither. With neither, the server signs with its own AWS credentials, from its shared credentials file or instance role. The secret key is never shown again once saved; leave it empty to keep it. |
| Clients load pictures from | **This server**, the default: every picture goes through the server. **The bucket directly**: clients are sent to the bucket, sparing the server sending them, where they can reach it. |
| Address clients reach it at | With The bucket directly, where clients reach the store, where that differs from Address, as for a store inside Docker reached by clients at another name. Empty for Address. |

Amazon S3 and Google Cloud Storage are addressed by the bucket's name in the host; every other store by its name in the path.

```sh
curl -X PUT https://photon.example/api/v1/admin/storage \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"kind": "bucket", "bucket": {"endpoint": "https://s3.example.com", "name": "photon",
       "access_key": "…", "secret_key": "…", "delivery": "proxy"}}'
```

`POST /api/v1/admin/storage/check` with the same body checks a bucket without choosing it.

### Sending clients to the bucket

With **The bucket directly**, a client asking for a picture, a theme tune or a preview is redirected to a link to it in the bucket, signed for an hour. Only pictures, sounds and videos are sent there, as their bytes said when they were put; anything that could run a script in a browser, a page or an SVG, still comes from the server.

Once saved, the page tries a picture in the bucket from your browser, and says "This browser loads pictures from the bucket", or that it cannot, and at which address. Where it cannot, set **Address clients reach it at**, or go back to This server.

## Move between the disk and a bucket

Choosing another place moves what is kept there:

1. Every server starts writing to both places at once, and reading from the old one.
2. Each server copies what its own disk holds, or, from one bucket to another, one server copies the bucket. The page shows each copy's progress: "1,204 of 5,880", then Copied.
3. Once every copy is done, every server keeps everything in the new place.

Nothing else can be chosen while a move runs. **Cancel move** stops it: every server goes on keeping things where they are now, and what was copied stays where it was copied to.

```sh
curl -X DELETE https://photon.example/api/v1/admin/storage/move -H "Authorization: Bearer $ADMIN_TOKEN"
```

A move waits on every server that is running, since each copies its own disk. Changing only the credentials or how clients are sent, for the same bucket and folder, is not a move: every server takes it up at once.

A server that cannot reach where things are kept when it starts refuses to start, and its log says why. One already running that loses the bucket goes on trying; each server reads the setting again every minute.
