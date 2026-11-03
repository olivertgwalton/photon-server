import type { components } from "#lib/api/schema.d.ts";

type Schemas = components["schemas"];

// What a browser says it plays of sound alone: the types its audio element
// and media sources take, and whether it plays HLS itself.
export type AudioCapabilities = { types: string[]; nativeHLS: boolean };

// Each question is a type a browser is asked about, and what a yes means in
// the server's words: ffprobe's codec and demuxer names.
const codecs = {
	aac: 'audio/mp4; codecs="mp4a.40.2"',
	opus: 'audio/mp4; codecs="opus"',
	flac: 'audio/mp4; codecs="flac"',
	alac: 'audio/mp4; codecs="alac"',
	eac3: 'audio/mp4; codecs="ec-3"',
	ac3: 'audio/mp4; codecs="ac-3"',
	mp3: "audio/mpeg",
	vorbis: 'audio/webm; codecs="vorbis"',
} as const;
const containers = {
	mp4: "audio/mp4",
	mp3: "audio/mpeg",
	flac: "audio/flac",
	ogg: "audio/ogg",
	wav: "audio/wav",
	aac: "audio/aac",
} as const;

const questions = [...Object.values(codecs), ...Object.values(containers)];

// Surround is sent where the browser decodes it; it mixes it down for the
// speakers it has, as the video player's profile assumes.
const surround = 6;

// The profile sent with a play of a track or an audiobook: no video, the
// sound the browser decodes in the files it opens, and each of a version's
// files in turn, as an audio element plays one file at a time. Anything else
// comes as HLS of sound alone, in fMP4.
export function audioProfile(
	caps: AudioCapabilities,
	maxBitrateKbps: number,
): Schemas["ClientProfile"] {
	const has = (type: string) => caps.types.includes(type);
	// Order is preference: the server encodes to the first it can.
	const audio = (Object.keys(codecs) as (keyof typeof codecs)[])
		.filter((codec) => has(codecs[codec]))
		.map((codec) => ({
			codec,
			max_channels: codec === "mp3" || codec === "vorbis" ? 2 : surround,
		}));
	return {
		containers: (Object.keys(containers) as (keyof typeof containers)[]).filter(
			(name) => has(containers[name]),
		),
		video: [],
		audio,
		max_bitrate_kbps: maxBitrateKbps,
		parts: "each",
		segments: "fmp4",
	};
}

// What this browser answers. A codec counts where the audio element plays it
// and, unless the browser plays HLS itself, where hls.js can feed it to the
// element through media sources too.
export function probeAudio(): AudioCapabilities {
	const element = document.createElement("audio");
	const appleHLS =
		navigator.vendor.startsWith("Apple") &&
		element.canPlayType("application/vnd.apple.mpegurl") !== "";
	const nativeHLS = appleHLS || !("MediaSource" in window);
	const plays = (type: string) =>
		element.canPlayType(type) !== "" &&
		(nativeHLS ||
			!type.includes("codecs") ||
			MediaSource.isTypeSupported(type));
	return { types: questions.filter(plays), nativeHLS };
}

// One thing the audio player plays: a track, or an audiobook's version.
export type Listen = {
	id: string;
	version?: string;
	title: string;
	// Who made it: a track's artists, a book's author.
	by?: string;
	// What it is from, its album, which the bar leads to.
	from?: { id: string; title: string };
	artwork?: string;
	duration_ms?: number;
	chapters?: Schemas["ChapterRef"][];
	// Where it opens: an audiobook, where the reader stopped.
	start_ms?: number;
};

export function trackListen(card: Schemas["Card"]): Listen {
	return {
		id: card.id,
		title: card.title,
		by: card.artists?.join(", ") || card.artist?.title,
		from: card.album ?? undefined,
		artwork: card.poster,
		duration_ms: card.duration_ms,
	};
}

// A track plays from its start again, rather than the one before, once it is
// this far in, as every music player's Previous does.
export const restartAfter = 3;

// Where Previous goes: to the start of where it is, a chapter's or the
// whole's, once past its first moments, else the chapter before; null where
// there is none before and it is the one before in the queue.
export function previousAt(
	seconds: number,
	chapters: Schemas["ChapterRef"][] = [],
): number | null {
	const ms = seconds * 1000;
	const at = chapters.findLastIndex((c) => c.start_ms <= ms);
	if (at < 0) return seconds > restartAfter ? 0 : null;
	const start = chapters[at].start_ms / 1000;
	if (seconds - start > restartAfter) return start;
	return at > 0 ? chapters[at - 1].start_ms / 1000 : null;
}

// Where Next goes within one thing: the next chapter's start, or null where
// there is none and it is the next in the queue.
export function nextAt(
	seconds: number,
	chapters: Schemas["ChapterRef"][] = [],
): number | null {
	const next = chapters.find((c) => c.start_ms > seconds * 1000);
	return next ? next.start_ms / 1000 : null;
}

export function chapterAt(
	seconds: number,
	chapters: Schemas["ChapterRef"][] = [],
): Schemas["ChapterRef"] | undefined {
	return chapters.findLast((c) => c.start_ms <= seconds * 1000);
}
