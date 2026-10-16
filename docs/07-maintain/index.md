---
title: "Maintain your server"
summary: "Keep photon-server backed up, up to date and healthy, and know what it does by itself."
---

photon-server looks after most of itself: it backs its database up every three days, scans its libraries, retries work that failed, and clears what nothing needs any more. What is left to an admin is knowing it is doing so, putting it back when something goes wrong, and moving it on to a new version. Everything here is under Settings › Server, for an admin.

## Pages

- [Back up and restore](01-backups.md): the database dumps the server makes, downloading one, and restoring from the web app or the command line.
- [Update photon-server](02-updating.md): a new version, its database migration, and going back.
- [Health, logs and metrics](03-health-and-metrics.md): `/health` for a balancer, the log, the dashboard, and Prometheus metrics.
- [Scheduled tasks, jobs and the maintenance window](04-tasks-and-jobs.md): what runs by itself and when, the job queue, and the hours that reading media waits for.
- [Storage](05-storage.md): where artwork, avatars, theme tunes and previews are kept, on each server's disk or in an S3-compatible bucket.
