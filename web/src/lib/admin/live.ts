import type { components } from "#lib/api/schema.js";

type Schemas = components["schemas"];
type Event = Schemas["Event"];
export type EventKind = Schemas["EventKind"];
export type NowPlaying = Schemas["NowPlaying"];
type Snapshot = Schemas["Snapshot"];

// What is going on now, as the admin event stream has told it: the snapshot it
// opens with, kept current by each event after it.
export type Live = Snapshot & {
	// Whether a snapshot has arrived, so a page knows to stop drawing what its
	// own load fetched.
	ready: boolean;
	// What the activity log kept that arrived since, the newest first.
	arrived: Event[];
};

export const idle: Live = {
	ready: false,
	tasks: [],
	jobs: [],
	backlogs: [],
	scans: [],
	playbacks: [],
	arrived: [],
};

// The kinds the activity log keeps (domain.EventKind.Logged).
const logged = new Set<EventKind>([
	"playback.started",
	"playback.stopped",
	"auth.signed_in",
	"auth.sign_in_refused",
	"profile.added",
	"profile.removed",
	"library.added",
	"library.removed",
	"library.scanned",
	"library.titles_added",
	"task.failed",
	"backup.made",
	"job.dead",
]);

// How many arrived events are kept; a page that wants more pages the log.
const keep = 50;

function without<T>(list: T[], drop: (item: T) => boolean): T[] {
	return list.filter((item) => !drop(item));
}

// The state after one named event of the stream.
export function apply(live: Live, name: string, data: unknown): Live {
	if (name === "snapshot") {
		return { ...(data as Snapshot), ready: true, arrived: live.arrived };
	}
	const e = data as Event;
	const next = logged.has(e.kind)
		? { ...live, arrived: [e, ...live.arrived].slice(0, keep) }
		: { ...live };
	const d = e.details;
	switch (e.kind) {
		case "playback.started":
		case "playback.paused":
		case "playback.resumed": {
			const p = d.playback as NowPlaying;
			const i = next.playbacks.findIndex((x) => x.id === p.id);
			next.playbacks =
				i < 0 ? [...next.playbacks, p] : next.playbacks.with(i, p);
			break;
		}
		case "playback.stopped": {
			const p = d.playback as NowPlaying;
			next.playbacks = without(next.playbacks, (x) => x.id === p.id);
			break;
		}
		case "scan.progress": {
			const scan = {
				library_id: e.library_id as string,
				phase: d.phase as Schemas["ScanPhase"],
				done: d.done as number,
				known: d.known as number,
				folder: d.folder as string | undefined,
			};
			next.scans = [
				...without(next.scans, (s) => s.library_id === scan.library_id),
				scan,
			];
			break;
		}
		case "library.scanned":
		case "library.removed":
			next.scans = without(next.scans, (s) => s.library_id === e.library_id);
			break;
		case "task.started":
			next.tasks = [
				...without(next.tasks, (t) => t.key === d.task),
				{ key: d.task as Schemas["TaskKey"], started_at: e.at },
			];
			break;
		case "task.finished":
		case "task.failed":
			next.tasks = without(next.tasks, (t) => t.key === d.task);
			break;
		case "job.started":
			next.jobs = [
				...without(next.jobs, (j) => j.id === d.job_id),
				{
					id: d.job_id as number,
					kind: d.job_kind as Schemas["JobKind"],
					subject: d.subject as string,
					attempt: d.attempt as number,
					title_id: e.title_id,
					library_id: e.library_id,
				},
			];
			break;
		case "jobs.progress": {
			const backlog = {
				kind: d.job_kind as Schemas["JobKind"],
				left: d.left as number,
				done: d.done as number,
			};
			const i = next.backlogs.findIndex((b) => b.kind === backlog.kind);
			if (backlog.left === 0) {
				next.backlogs = without(next.backlogs, (b) => b.kind === backlog.kind);
			} else {
				next.backlogs =
					i < 0 ? [...next.backlogs, backlog] : next.backlogs.with(i, backlog);
			}
			break;
		}
		case "job.finished":
		case "job.failed":
		case "job.dead":
			next.jobs = without(next.jobs, (j) => j.id === d.job_id);
			// A scan that failed tells no library.scanned; its job ending is the end.
			if (d.job_kind === "scan_library") {
				next.scans = without(next.scans, (s) => s.library_id === e.library_id);
			}
			break;
	}
	return next;
}

// Where a playing title is now: the position it last reported, moved on by the
// time since while it plays, as no event tells each second of it.
export function positionAt(p: NowPlaying, now: number): number {
	if (p.state !== "playing") return p.position_ms;
	const moved = now - Date.parse(p.updated_at);
	return Math.min(p.position_ms + Math.max(moved, 0), p.version.duration_ms);
}
