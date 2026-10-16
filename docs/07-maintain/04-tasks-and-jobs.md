---
title: "Scheduled tasks, jobs and the maintenance window"
summary: "What photon-server does by itself and when, the queue of work behind it, and the hours its reading of media waits for."
---

photon-server's background work comes in two layers. **Scheduled tasks** run on a timetable and decide what needs doing: scan the libraries, find the titles due a fresh match, back the database up. Many of them do it by queueing **jobs**, one per library, title, season or file, which every server works through. The **maintenance window** sets the hours the heaviest of those jobs, reading whole media files, are held to. This page covers all three.

## Scheduled tasks

Settings › Server › Scheduled tasks lists each task, when it last ran, how long it took and how it ended, and when it next runs.

| Task | What it does | When |
|---|---|---|
| Scan libraries | Reads every library's folders for what is new, changed or gone. | Every 12 hours |
| Requeue stalled jobs | Puts back the jobs a node stopped working on. | Every minute |
| Back up the database | Dumps the database, keeping the newest three. | Every 3 days |
| Refresh metadata | Asks the providers again about titles whose libraries say it is time. | Daily at 03:00 |
| Clear old artwork | Removes replaced pictures from the cache, takes the blur shown while each loads, and fetches pictures the cache lacks. | Every 7 days |
| Detect intros and credits | Compares the sound of each season's episodes, and finds where each film's credits begin. | Daily, as the maintenance window opens |
| Make previews | Makes the chapter images and seek previews libraries ask for, and clears unused ones. | Daily, as the maintenance window opens |
| Clear old downloads | Forgets downloads kept past their week, and conversions nothing needs. | Every hour |
| Prune the activity log | Forgets activity older than 30 days. | Every day |
| Refresh smart collections | Finds what each smart collection's filters hold again. | Every 15 minutes |
| Sync list collections | Reads each list collection's TMDB or MDBList list again. | Every day |
| Download missing subtitles | Fetches subtitles in the languages libraries name for copies with none. | Every day |

A task on an interval runs once that long has passed since it last started, and at once on a new server. **Refresh metadata** runs at 03:00 in the server's own time zone. A run missed while the server was down runs at the next chance, not at the next time it falls due. Each task runs one at a time.

Some tasks also run when something calls for them: a scan that adds titles to a library with subtitle languages asks for **Download missing subtitles** straight away, and a scan refreshes its library's smart collections itself.

With several servers, one node runs the scheduled tasks, whichever holds the scheduler's lease; if it stops, another takes over within 20 seconds. The jobs a task queues are worked by every node.

### Run a task now

**Run now** runs a task as soon as it is not running, on whichever node runs the tasks. What it finds to do starts at once, outside the maintenance window too, and runs to its end: asking is taken to mean you want it now.

```sh
curl -X POST https://photon.example/api/v1/admin/tasks/scan_libraries/run \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

The tasks' keys are `scan_libraries`, `sweep_jobs`, `backup_database`, `refresh_metadata`, `sweep_artwork`, `detect_markers`, `backfill_previews`, `sweep_downloads`, `prune_activity`, `refresh_collections`, `sync_lists` and `fetch_subtitles`.

### Stop a task's work

The four tasks that queue jobs (Scan libraries, Refresh metadata, Detect intros and credits and Make previews) show their progress beneath their name, "42% · 1,260 of 3,000 · 1 running", and a **Stop** button while their jobs run. Stopping takes the jobs left off the queue, and one running stops within a few minutes. What is done is kept; the task's next run, or Run now, takes up the rest.

```sh
curl -X POST https://photon.example/api/v1/admin/tasks/backfill_previews/stop \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

The other tasks do all they do in moments, and asking to stop one is answered `409`.

## Jobs

Settings › Server › Jobs counts the queue by kind and state, and lists the jobs given up on.

| Job | Queued by |
|---|---|
| Scan a library | A scan, by the task, the library's page or a watched folder changing. |
| Identify | A new title, or one due a refresh. |
| Read media info | Analyse, on a title. |
| Read keyframes, Walk files for keyframes | A scan, as the library's Keyframes setting says. |
| Find intros and credits, Ask plugins for intros and credits | Detect intros and credits, or a scan. |
| Make previews | Make previews, or a scan. |
| Fetch a theme tune | A match, where the library takes themes. |
| Convert for download | A [download](../05-playback/03-downloads.md) that needs converting. |
| Send a webhook | An event a [webhook](../06-integrations/05-webhooks.md) asks for. |
| Import watch history | An [import](../06-integrations/07-import-history.md). |

The states are **Queued**, **Running**, **To run again** (running, but its subject changed since it started, so it is queued again when it ends) and **Gave up**.

Each kind of job has slots of its own on each node, so a long queue of one never holds up another: two library scans at once, sixteen matches, two webhook deliveries, one import, one conversion, and one of each kind that reads media.

### When a job fails

A job that fails is tried again after a wait that doubles each time, up to an hour, five times in all. After the fifth it is given up on: Settings › Server › Dashboard says so under **Needs attention**, and the job waits on Settings › Server › Jobs, with its last error, until its subject changes or you choose **Try again**, which gives it five more attempts at once.

```sh
curl -X POST https://photon.example/api/v1/admin/jobs/1234/retry -H "Authorization: Bearer $ADMIN_TOKEN"
```

A job that could not start for want of room, a playback in the way or the window closing, is put back without losing an attempt. A node that stops mid-job leaves it held for up to 10 minutes, then **Requeue stalled jobs** puts it back for any node.

## The maintenance window

The jobs that read media files through, **Read keyframes**, **Walk files for keyframes**, **Find intros and credits**, **Make previews** and **Read media info**, are kept out of playback's way:

- None starts while anything is playing on any node, and one running stops when a play starts, to be queued again. A still, a fingerprint or a walk for keyframes reads the same disk or network mount a stream reads, and several at once can starve the stream.
- Those queued for the window start only inside it, and stop as it closes.

Set it at the top of Settings › Server › Scheduled tasks, under **Maintenance window**:

| Setting | What it does |
|---|---|
| From | The hour the window opens. 02:00 by default. |
| Until | The hour it closes. 05:00 by default. An hour earlier than From runs past midnight. |
| Time zone | The zone those hours are in, by its IANA name. UTC by default. Every node keeps the window in this zone. |
| Make previews | **In the window**, the default: chapter images and seek previews of new titles wait for it. **In the window and as titles are added**: a scan queues them at once. |
| Find intros and credits | **In the window and as titles are added**, the default: a scan queues new episodes and films at once. **In the window**: they wait for it. |

Either way, a library whose own Previews or Intros and credits setting is off makes none. What the window's own runs of Detect intros and credits and Make previews queue is for the window; what Run now queues is due at once. A library set to read keyframes by reading every file through does so in the window.

```sh
curl -X PUT https://photon.example/api/v1/admin/maintenance \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"start_hour": 1, "end_hour": 7, "time_zone": "Europe/London",
       "previews": "window", "markers": "window_and_added"}'
```

Every node takes up a change at once. Matching, scans, webhooks, imports and conversions are not held to the window or by playback: someone is waiting on each.
