import type { components } from "./api/schema.d.ts";

type InboxChange = components["schemas"]["InboxChange"];

// What a page's data depends on, so a change reloads only the pages it touches.
export const keys = {
	home: "photon:home",
	// The profile's own marks, which every wall, row and list draws.
	userdata: "photon:userdata",
	playlists: "photon:playlists",
	downloads: "photon:downloads",
	// The profile's accounts on its trackers, and the codes it is entering.
	trackers: "photon:trackers",
	// The requests waiting for the profile to decide them.
	requests: "photon:requests",
	library: (id: string) => `photon:library:${id}` as const,
	title: (id: string) => `photon:title:${id}` as const,
	// What the dashboard's pages load, as the admin stream tells of it.
	admin: {
		tasks: "admin:tasks",
		jobs: "admin:jobs",
		playbacks: "admin:playbacks",
		libraries: "admin:libraries",
		profiles: "admin:profiles",
		backups: "admin:backups",
	},
} as const;

// One event of the profile's feed on the server's events socket, shaped as the
// activity log's events are.
export type Change = {
	kind: string;
	library_id?: string;
	title_id?: string;
	details?: {
		phase?: "reading" | "removing";
		done?: number;
		known?: number;
		folder?: string;
	};
};

// The data a change makes stale.
export function affected(change: Change): `photon:${string}`[] {
	const out = new Set<`photon:${string}`>();
	const lib = change.library_id;
	const title = change.title_id;
	switch (change.kind) {
		case "library.changed":
		case "library.scanned":
			out.add(keys.home);
			if (lib) out.add(keys.library(lib));
			break;
		case "title.updated":
			out.add(keys.home);
			if (title) out.add(keys.title(title));
			if (lib) out.add(keys.library(lib));
			break;
		case "userdata.changed":
			out.add(keys.home);
			out.add(keys.userdata);
			if (title) out.add(keys.title(title));
			break;
		case "tracker.changed":
			out.add(keys.trackers);
			break;
	}
	return [...out];
}

// The data a change of the profile's inbox makes stale: a request told as
// waiting, or one cleared, which may be a request someone decided.
export function inboxAffected(change: InboxChange): `photon:${string}`[] {
	const asked = change.notifications?.some((n) => n.kind === "request");
	return asked || change.change === "cleared" ? [keys.requests] : [];
}

// The dashboard's data an admin event makes stale, by the event's name.
const adminChanges: Record<string, string> = {
	"task.started": keys.admin.tasks,
	"task.finished": keys.admin.tasks,
	"task.failed": keys.admin.tasks,
	"job.started": keys.admin.jobs,
	"job.finished": keys.admin.jobs,
	"job.failed": keys.admin.jobs,
	"job.dead": keys.admin.jobs,
	"playback.started": keys.admin.playbacks,
	"playback.stopped": keys.admin.playbacks,
	"library.added": keys.admin.libraries,
	"library.removed": keys.admin.libraries,
	"profile.added": keys.admin.profiles,
	"profile.removed": keys.admin.profiles,
	"backup.made": keys.admin.backups,
};

export function adminAffected(event: string): string[] {
	const key = adminChanges[event];
	return key ? [key] : [];
}
