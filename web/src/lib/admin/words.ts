import type { components } from "#lib/api/schema.js";
import { fullTitle } from "#lib/format.js";

type Schemas = components["schemas"];

// What each part of the server is called on the dashboard. The API names
// things by key; these are the words an admin reads.

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
	manager: "Manager",
	user: "User",
};

// The roles a profile of this role may give: an admin any, a manager users.
export function roleOptions(giver: Schemas["Role"]) {
	return Object.entries(roles)
		.filter(([value]) => giver === "admin" || value === "user")
		.map(([value, label]) => ({ value: value as Schemas["Role"], label }));
}

// Each item's name by its id, for lists that name what an event was about.
export function byName(
	items: readonly { id: string; name: string }[],
): Map<string, string> {
	return new Map(items.map((i) => [i.id, i.name]));
}

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

export const importSources: Record<Schemas["ImportSource"], string> = {
	plex: "Plex",
	jellyfin: "Jellyfin",
	emby: "Emby",
};

// Why a title another server had watched was not imported.
export const importMisses: Record<Schemas["ImportMiss"], string> = {
	no_ids: "no TMDB, TheTVDB or IMDb id",
	not_found: "not here",
	undated: "no date watched",
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

const units: [
	Intl.RelativeTimeFormatUnit & ("day" | "hour" | "minute"),
	number,
][] = [
	["day", 86_400],
	["hour", 3_600],
	["minute", 60],
];

// How long ago, or how far ahead, a moment is, in its largest whole unit.
export function relative(at: string | number, now: number): string {
	const seconds = Math.round(
		((typeof at === "number" ? at : Date.parse(at)) - now) / 1000,
	);
	const format = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
	for (const [unit, size] of units) {
		if (Math.abs(seconds) >= size)
			return format.format(Math.trunc(seconds / size), unit);
	}
	return format.format(seconds, "second");
}

// How long something has run, in its largest whole unit: "3 days", "5 minutes".
export function elapsed(ms: number): string {
	const seconds = Math.max(0, Math.round(ms / 1000));
	const [unit, size] = units.find(([, size]) => seconds >= size) ?? [
		"second",
		1,
	];
	return new Intl.NumberFormat(undefined, {
		style: "unit",
		unit,
		unitDisplay: "long",
	}).format(Math.trunc(seconds / size));
}

export const when = new Intl.DateTimeFormat(undefined, {
	dateStyle: "medium",
	timeStyle: "short",
});
