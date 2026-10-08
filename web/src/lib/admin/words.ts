import type { components } from "#lib/api/schema.js";
import { fullTitle } from "#lib/format.js";

type Schemas = components["schemas"];

// What each part of the server is called on the dashboard. The API names
// things by key; these are the words an admin reads.

export const tasks: Record<Schemas["TaskKey"], { name: string; does: string }> =
	{
		scan_libraries: {
			name: "Scan libraries",
			does: "Reads every library's folders for what is new, changed or gone.",
		},
		sweep_jobs: {
			name: "Requeue stalled jobs",
			does: "Puts back the jobs a node stopped working on.",
		},
		backup_database: {
			name: "Back up the database",
			does: "Dumps the database for pg_restore, keeping the newest few.",
		},
		refresh_metadata: {
			name: "Refresh metadata",
			does: "Asks the providers again about titles whose libraries say it is time.",
		},
		sweep_artwork: {
			name: "Clear old artwork",
			does: "Removes replaced pictures from the cache, takes the blur drawn while each loads where it has none, and fetches the pictures titles show that the cache lacks.",
		},
		detect_markers: {
			name: "Detect intros and credits",
			does: "Compares the sound of each season's episodes to find what they share, and finds where each film's picture goes dark for its credits.",
		},
		backfill_previews: {
			name: "Make previews",
			does: "Makes the chapter images and seek previews libraries ask for, and clears unused ones.",
		},
		sweep_downloads: {
			name: "Clear old downloads",
			does: "Forgets downloads kept past their time, and conversions nothing needs.",
		},
		prune_activity: {
			name: "Prune the activity log",
			does: "Forgets activity older than 30 days.",
		},
		sync_lists: {
			name: "Sync list collections",
			does: "Reads each list collection's TMDB or MDBList list again and keeps the titles of it the library has.",
		},
		refresh_collections: {
			name: "Refresh smart collections",
			does: "Finds what each smart collection's filters hold again, catching what was matched or edited since.",
		},
		fetch_subtitles: {
			name: "Download missing subtitles",
			does: "Fetches subtitles in the languages libraries name for copies with none in them.",
		},
	};

export const jobKinds: Record<Schemas["JobKind"], string> = {
	keyframes: "Read keyframes",
	keyframe_walk: "Walk files for keyframes",
	identify: "Identify",
	scan_library: "Scan a library",
	markers: "Find intros and credits",
	previews: "Make previews",
	convert: "Convert for download",
	deliver_webhook: "Send a webhook",
	theme: "Fetch a theme tune",
	probe: "Read media info",
	import_history: "Import watch history",
};

export const reasons: Record<Schemas["TranscodeReason"], string> = {
	container_not_supported: "container",
	video_codec_not_supported: "video codec",
	video_profile_not_supported: "video profile",
	video_level_not_supported: "video level",
	video_resolution_not_supported: "resolution",
	video_bit_depth_not_supported: "bit depth",
	video_range_not_supported: "HDR",
	audio_codec_not_supported: "audio codec",
	audio_channels_not_supported: "audio channels",
	bitrate_exceeds_limit: "bitrate",
	subtitle_codec_not_supported: "subtitles",
	parts_not_supported: "several files",
};

export const accelerations: Record<Schemas["Acceleration"], string> = {
	software: "Software",
	videotoolbox: "VideoToolbox",
	vaapi: "VA-API",
	qsv: "Quick Sync",
	nvenc: "NVENC",
};

// Where a node's limit on transcodes at once comes from.
export const limitSources: Record<Schemas["LimitSource"], string> = {
	automatic: "worked out from the encoder",
	set: "set here",
};

// What a node does for the server, in a few words and in a sentence.
export const nodeRoles: Record<
	Schemas["NodeRole"],
	{ name: string; description: string }
> = {
	all: {
		name: "Serves and transcodes",
		description: "As every server on its own does.",
	},
	transcode: {
		name: "Transcodes first",
		description:
			"Asked to transcode before any other, while it has room: the server with the GPU. It serves people too.",
	},
	serve: {
		name: "Serves only",
		description:
			"Transcodes nothing, for playback or downloads, leaving that to the others.",
	},
};

// Whether a node takes new streams, and how far one draining has got.
export function nodeAvailability(n: Schemas["KnownNode"]): string {
	if (n.availability === "active") return "Takes new streams";
	const left = n.online?.transcodes ?? 0;
	if (!left) return "Drained · safe to stop";
	return `Draining · ${left === 1 ? "1 stream" : `${left} streams`} left`;
}

// Videos being transcoded of the most at once, as words: "3 of 8", or "3, no limit".
export function transcodeLoad(active: number, limit?: number): string {
	return limit ? `${active} of ${limit}` : `${active}, no limit`;
}

export const roles: Record<Schemas["Role"], string> = {
	admin: "Admin",
	member: "Member",
	restricted: "Restricted",
};

export const extraKinds: Record<Schemas["ExtraKind"], string> = {
	trailer: "Trailers",
	teaser: "Teasers",
	featurette: "Featurettes",
	behind_the_scenes: "Behind the scenes",
	deleted_scene: "Deleted scenes",
	interview: "Interviews",
	scene: "Scenes",
	short: "Shorts",
	clip: "Clips",
	blooper: "Bloopers",
	theme_video: "Theme videos",
	other: "Other",
};

export const markerKinds: Record<Schemas["MarkerKind"], string> = {
	intro: "Intro",
	credits: "Credits",
	recap: "Recap",
	preview: "Preview",
};

// The kinds a webhook may be told of (domain.EventKind.Hookable), and what each is.
export const hookable: Partial<Record<Schemas["EventKind"], string>> = {
	"playback.started": "A play starts",
	"playback.paused": "A play is paused",
	"playback.resumed": "A play is resumed",
	"playback.stopped": "A play stops",
	"auth.signed_in": "Someone signs in",
	"auth.sign_in_refused": "A sign-in is refused",
	"profile.added": "A profile is added",
	"profile.removed": "A profile is removed",
	"library.added": "A library is added",
	"library.removed": "A library is removed",
	"library.scanned": "A library is scanned",
	"library.titles_added": "Titles are added",
	"task.failed": "A task fails",
	"backup.made": "A backup is made",
};

// The kinds the activity log keeps, as its filter offers them.
export const loggedKinds: Partial<Record<Schemas["EventKind"], string>> = {
	"playback.started": "Plays started",
	"playback.stopped": "Plays stopped",
	"auth.signed_in": "Sign-ins",
	"auth.sign_in_refused": "Refused sign-ins",
	"profile.added": "Profiles added",
	"profile.removed": "Profiles removed",
	"library.added": "Libraries added",
	"library.removed": "Libraries removed",
	"library.scanned": "Scans",
	"library.titles_added": "Titles added",
	"task.failed": "Failed tasks",
	"backup.made": "Backups",
	"job.dead": "Dead jobs",
};

// A played title as one line: a show's episode by its show and place in it.
export function playedTitle(t: Schemas["PlaybackTitle"]): string {
	return fullTitle(t, t.kind === "episode" ? t.show : undefined);
}

type Names = {
	profiles: Map<string, string>;
	libraries: Map<string, string>;
};

// One event as a sentence for the activity log.
export function describe(e: Schemas["Event"], names: Names): string {
	const d = e.details as Record<string, unknown>;
	const profile =
		(e.profile_id && names.profiles.get(e.profile_id)) || "Someone";
	const library =
		(e.library_id && names.libraries.get(e.library_id)) || "A library";
	const playback = d.playback as Schemas["NowPlaying"] | undefined;
	const played = playback ? playedTitle(playback.title) : "a title";
	const by = playback?.profile.name ?? profile;
	switch (e.kind) {
		case "playback.started":
			return `${by} started ${played}`;
		case "playback.paused":
			return `${by} paused ${played}`;
		case "playback.resumed":
			return `${by} resumed ${played}`;
		case "playback.stopped":
			return d.reach === "end"
				? `${by} finished ${played}`
				: `${by} stopped ${played}`;
		case "auth.signed_in":
			return `${d.name} signed in on ${d.device} (${d.client})`;
		case "auth.sign_in_refused":
			return `A sign-in as ${d.name} from ${d.address} was refused`;
		case "profile.added":
			return `Profile ${d.name} was added`;
		case "profile.removed":
			return `Profile ${d.name} was removed`;
		case "library.added":
			return `Library ${d.name} was added`;
		case "library.removed":
			return `Library ${d.name} was removed`;
		case "library.scanned":
			return `${library} was scanned: ${d.folders} folders, ${d.probed} read`;
		case "library.titles_added":
			return `${d.titles} ${d.titles === 1 ? "title was" : "titles were"} added to ${library}`;
		case "scan.progress":
			return `${library} is scanning`;
		case "library.changed":
			return `${library} changed`;
		case "title.updated":
			return "A title was described again";
		case "userdata.changed":
			return `${profile}'s watching changed`;
		case "task.started":
			return `${taskName(d.task)} started`;
		case "task.finished":
			return `${taskName(d.task)} finished`;
		case "task.failed":
			return `${taskName(d.task)} failed: ${d.error}`;
		case "backup.made":
			return `The database was backed up to ${d.file}`;
		case "job.started":
			return `${jobName(d.job_kind)} started`;
		case "job.finished":
			return `${jobName(d.job_kind)} finished`;
		case "job.failed":
			return `${jobName(d.job_kind)} failed and will be tried again: ${d.error}`;
		case "job.dead":
			return `${jobName(d.job_kind)} gave up after ${d.attempt} tries: ${d.error}`;
		case "jobs.progress":
			return `${jobName(d.job_kind)}: ${d.left} left`;
		case "webhook.test":
			return "A webhook test was sent";
		case "maintenance.changed":
			return "The maintenance window was changed";
		case "network.changed":
			return "Secure connections were changed";
		case "storage.changed":
			return "Where artwork and previews are kept was changed";
		case "nodes.changed":
			return "What a server node does was changed";
	}
}

function taskName(key: unknown): string {
	return tasks[key as Schemas["TaskKey"]]?.name ?? String(key);
}

function jobName(kind: unknown): string {
	return jobKinds[kind as Schemas["JobKind"]] ?? String(kind);
}

// The reverse of timecode: "1:02:03", "2:03" or "3" seconds, with an optional
// fraction; undefined for anything else.
export function parseClock(text: string): number | undefined {
	const match = /^(?:(?:(\d+):)?(\d{1,2}):)?(\d{1,2}(?:\.\d{1,3})?)$/.exec(
		text.trim(),
	);
	if (!match) return undefined;
	const [, h = "0", m = "0", s] = match;
	return Math.round((Number(h) * 3600 + Number(m) * 60 + Number(s)) * 1000);
}

export const bytes = new Intl.NumberFormat(undefined, {
	style: "unit",
	unit: "gigabyte",
	maximumFractionDigits: 1,
});

// How long ago, or how far ahead, a moment is, in its largest whole unit.
export function relative(at: string, now: number): string {
	const seconds = Math.round((Date.parse(at) - now) / 1000);
	const units: [Intl.RelativeTimeFormatUnit, number][] = [
		["day", 86_400],
		["hour", 3_600],
		["minute", 60],
	];
	const format = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
	for (const [unit, size] of units) {
		if (Math.abs(seconds) >= size)
			return format.format(Math.trunc(seconds / size), unit);
	}
	return format.format(seconds, "second");
}

export const when = new Intl.DateTimeFormat(undefined, {
	dateStyle: "medium",
	timeStyle: "short",
});
