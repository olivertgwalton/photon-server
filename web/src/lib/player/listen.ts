import type { components } from "#lib/api/schema.d.ts";
import { shuffled } from "#lib/shuffle.ts";

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
	// The artwork's blurhash, whose colour the Now Playing view takes.
	blurhash?: string;
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
		blurhash: card.poster ? card.blurhashes?.[card.poster] : undefined,
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

// One place in the queue. A track may be queued twice, so each has a key of
// its own: a count, as a page served over plain HTTP has no crypto.randomUUID.
export type Queued = Listen & { key: string };

let keys = 0;

export function queued(items: Listen[]): Queued[] {
	return items.map((item) => ({ ...item, key: `q${++keys}` }));
}

// A queue and where in it the player is: what played before, what plays
// after.
export type Queue = { items: Queued[]; at: number };

// Things put straight after the one playing, as Plexamp's and Finamp's Play
// Next do.
export function playingNext(q: Queue, items: Queued[]): Queue {
	return { items: q.items.toSpliced(q.at + 1, 0, ...items), at: q.at };
}

// One moved from where it was to where it is dropped, the one playing still
// playing.
export function movedTo(q: Queue, from: number, to: number): Queue {
	const playing = q.items[q.at];
	const items = q.items.toSpliced(from, 1);
	items.splice(Math.max(0, Math.min(to, items.length)), 0, q.items[from]);
	return { items, at: items.findIndex((i) => i.key === playing.key) };
}

// One taken out. Taking out the one playing leaves the player at the one
// after it, or, where it was the last, the one before.
export function without(q: Queue, index: number): Queue {
	const items = q.items.toSpliced(index, 1);
	const at = index < q.at ? q.at - 1 : q.at;
	return { items, at: Math.min(at, Math.max(items.length - 1, 0)) };
}

// What is still to play, shuffled or put back in the order it was queued in,
// what played and the one playing staying where they are.
export function reshuffled(
	q: Queue,
	order: "shuffled" | "in_order",
	original: Queued[],
): Queue {
	const done = q.items.slice(0, q.at + 1);
	const left = q.items.slice(q.at + 1);
	const leftKeys = new Set(left.map((i) => i.key));
	const next =
		order === "shuffled"
			? shuffled(left)
			: original.filter((i) => leftKeys.has(i.key));
	return { items: [...done, ...next], at: q.at };
}

// What plays after the end of one: the next, or with the queue played
// through, its first again where it repeats.
export type Repeat = "off" | "all" | "one";

export function after(q: Queue, repeat: Repeat): number | null {
	if (repeat === "one") return q.at;
	if (q.at < q.items.length - 1) return q.at + 1;
	return repeat === "all" && q.items.length ? 0 : null;
}

// How music is sent: each file as it is, or no more than a bitrate, encoded
// where it is more, as Finamp's transcoding setting has it.
export const qualities = ["original", "320", "192", "128"] as const;
export type Quality = (typeof qualities)[number];

export function qualityKbps(quality: Quality): number {
	return quality === "original" ? 0 : Number(quality);
}

// The keys the player answers anywhere in the shell, as YouTube's and
// Plexamp's: k or Space to play and pause, j and l to go back and on ten
// seconds, Shift+N and Shift+P for the next and the one before, m to mute.
export type KeyAction =
	| "toggle"
	| "back"
	| "forward"
	| "next"
	| "previous"
	| "mute";

export function keyAction(e: {
	key: string;
	shiftKey: boolean;
	ctrlKey: boolean;
	metaKey: boolean;
	altKey: boolean;
}): KeyAction | undefined {
	if (e.ctrlKey || e.metaKey || e.altKey) return undefined;
	if (e.shiftKey) {
		if (e.key === "N") return "next";
		if (e.key === "P") return "previous";
		return undefined;
	}
	switch (e.key) {
		case " ":
		case "k":
			return "toggle";
		case "j":
			return "back";
		case "l":
			return "forward";
		case "m":
			return "mute";
		default:
			return undefined;
	}
}

// Which of a track's ReplayGains it is played by, as Finamp's setting has
// it: its own, so every track is as loud as the next, or its album's, so an
// album keeps the quiet and loud of its tracks.
export const gainModes = ["track", "album", "off"] as const;
export type GainMode = (typeof gainModes)[number];

// What the volume is turned by. A gain up is not turned up past full: an
// audio element's volume goes no higher, and Safari plays HLS through the
// Web Audio API as silence.
export function gainFactor(
	gain: Schemas["ReplayGain"] | null | undefined,
	mode: GainMode,
): number {
	if (!gain || mode === "off") return 1;
	const [db, peak] =
		mode === "album" && gain.album_gain_db != null
			? [gain.album_gain_db, gain.album_peak]
			: [gain.track_gain_db ?? gain.album_gain_db, gain.track_peak];
	if (db == null) return 1;
	// Never so loud that the loudest sample clips.
	const ceiling = peak ? 1 / peak : 1;
	return Math.min(10 ** (db / 20), ceiling, 1);
}

// The line of timed lyrics being sung at ms: the last to have started, -1
// before the first.
export function lineAt(
	lines: { start_ms?: number | null }[],
	ms: number,
): number {
	return lines.findLastIndex((l) => (l.start_ms ?? 0) <= ms);
}
