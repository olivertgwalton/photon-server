import { expect, test } from "bun:test";
import {
	type AudioCapabilities,
	after,
	audioProfile,
	gainFactor,
	keptOf,
	keyAction,
	lineAt,
	movedTo,
	nextAt,
	playingNext,
	previousAt,
	type Queue,
	qualityKbps,
	readKept,
	reshuffled,
	without,
} from "./listen.ts";

const a = (codecs: string) => `audio/mp4; codecs="${codecs}"`;

// What each browser's audio element answered on a Mac, November 2026.
const chrome: AudioCapabilities = {
	types: [
		...["mp4a.40.2", "opus", "flac"].map(a),
		"audio/mpeg",
		'audio/webm; codecs="vorbis"',
		"audio/mp4",
		"audio/flac",
		"audio/ogg",
		"audio/wav",
		"audio/aac",
	],
	nativeHLS: false,
};
const safari: AudioCapabilities = {
	types: [
		...["mp4a.40.2", "flac", "alac", "ec-3", "ac-3", "opus"].map(a),
		"audio/mpeg",
		"audio/mp4",
		"audio/flac",
		"audio/wav",
		"audio/aac",
	],
	nativeHLS: true,
};

test("a play asks for sound alone, each file in turn, cut into fMP4", () => {
	const p = audioProfile(chrome, 0);
	expect(p.video).toEqual([]);
	expect(p.parts).toBe("each");
	expect(p.segments).toBe("fmp4");
});

test("chrome is sent its FLAC and Ogg as they are, AAC first where encoded", () => {
	const p = audioProfile(chrome, 0);
	expect(p.audio.map((s) => s.codec)).toEqual([
		"aac",
		"opus",
		"flac",
		"mp3",
		"vorbis",
	]);
	expect(p.containers).toEqual(["mp4", "mp3", "flac", "ogg", "wav", "aac"]);
});

test("safari is sent ALAC and Dolby, and no Ogg it cannot open", () => {
	const p = audioProfile(safari, 0);
	expect(p.audio.map((s) => s.codec)).toContain("alac");
	expect(p.audio.map((s) => s.codec)).toContain("eac3");
	expect(p.containers).not.toContain("ogg");
});

test("stereo codecs are never sent surround", () => {
	const p = audioProfile(chrome, 0);
	expect(p.audio.find((s) => s.codec === "mp3")?.max_channels).toBe(2);
	expect(p.audio.find((s) => s.codec === "aac")?.max_channels).toBe(6);
});

const chapters = [
	{ start_ms: 0, end_ms: 60_000, title: "One" },
	{ start_ms: 60_000, end_ms: 120_000, title: "Two" },
	{ start_ms: 120_000, end_ms: 180_000, title: "Three" },
];

test("previous starts a track again once it is a few seconds in", () => {
	expect(previousAt(42)).toBe(0);
	// At its start, it is the one before.
	expect(previousAt(1)).toBeNull();
});

test("previous starts a chapter again, or goes to the one before", () => {
	expect(previousAt(90, chapters)).toBe(60);
	expect(previousAt(61, chapters)).toBe(0);
	// The first chapter's start is the book's, and the one before is the queue's.
	expect(previousAt(2, chapters)).toBeNull();
});

test("next goes to the next chapter, and past the last to the next in the queue", () => {
	expect(nextAt(10, chapters)).toBe(60);
	expect(nextAt(130, chapters)).toBeNull();
	expect(nextAt(10)).toBeNull();
});

const q = (...ids: string[]) =>
	ids.map((id) => ({ id, title: id, key: `k-${id}` }));
const ids = (queue: Queue) => queue.items.map((i) => i.id);

test("play next goes straight after the one playing", () => {
	const next = playingNext({ items: q("a", "b", "c"), at: 1 }, q("x", "y"));
	expect(ids(next)).toEqual(["a", "b", "x", "y", "c"]);
	expect(next.at).toBe(1);
});

test("moving one keeps the one playing playing", () => {
	const moved = movedTo({ items: q("a", "b", "c", "d"), at: 1 }, 3, 0);
	expect(ids(moved)).toEqual(["d", "a", "b", "c"]);
	expect(moved.items[moved.at].id).toBe("b");
	const self = movedTo({ items: q("a", "b", "c"), at: 0 }, 0, 2);
	expect(ids(self)).toEqual(["b", "c", "a"]);
	expect(self.at).toBe(2);
});

test("taking one out leaves the player where it was", () => {
	expect(without({ items: q("a", "b", "c"), at: 2 }, 0)).toMatchObject({
		at: 1,
	});
	// The one playing gone, the one after plays.
	const gone = without({ items: q("a", "b", "c"), at: 1 }, 1);
	expect(gone.items[gone.at].id).toBe("c");
	// The last gone, the one before.
	expect(without({ items: q("a", "b"), at: 1 }, 1).at).toBe(0);
});

test("shuffling leaves what played, and in order puts the rest back", () => {
	const original = q("a", "b", "c", "d", "e");
	const shuffled = reshuffled({ items: original, at: 1 }, "shuffled", original);
	expect(ids(shuffled).slice(0, 2)).toEqual(["a", "b"]);
	expect(ids(shuffled).toSorted()).toEqual(["a", "b", "c", "d", "e"]);
	const back = reshuffled(
		{ items: [original[0], original[1], original[4], original[2]], at: 1 },
		"in_order",
		original,
	);
	expect(ids(back)).toEqual(["a", "b", "c", "e"]);
});

test("the queue played through ends, or starts again where it repeats", () => {
	const end = { items: q("a", "b"), at: 1 };
	expect(after(end, "off")).toBeNull();
	expect(after(end, "all")).toBe(0);
	expect(after(end, "one")).toBe(1);
	expect(after({ items: q("a", "b"), at: 0 }, "off")).toBe(1);
});

test("the original quality sends no limit", () => {
	expect(qualityKbps("original")).toBe(0);
	expect(qualityKbps("192")).toBe(192);
});

test("the player's keys, and none with a modifier the browser keeps", () => {
	const key = (k: string, shift = false) => ({
		key: k,
		shiftKey: shift,
		ctrlKey: false,
		metaKey: false,
		altKey: false,
	});
	expect(keyAction(key(" "))).toBe("toggle");
	expect(keyAction(key("N", true))).toBe("next");
	expect(keyAction(key("P", true))).toBe("previous");
	expect(keyAction(key("l"))).toBe("forward");
	expect(keyAction({ ...key("k"), metaKey: true })).toBeUndefined();
	expect(keyAction(key("x"))).toBeUndefined();
});

test("a track is turned down by its own gain, or its album's", () => {
	const gain = { track_gain_db: -6, album_gain_db: -9 };
	expect(gainFactor(gain, "track")).toBeCloseTo(0.501, 3);
	expect(gainFactor(gain, "album")).toBeCloseTo(0.355, 3);
	expect(gainFactor(gain, "off")).toBe(1);
	// An album's gain where the track has none of its own, and the other way.
	expect(gainFactor({ album_gain_db: -6 }, "track")).toBeCloseTo(0.501, 3);
	expect(gainFactor({ track_gain_db: -6 }, "album")).toBeCloseTo(0.501, 3);
	expect(gainFactor(undefined, "track")).toBe(1);
});

test("a gain up goes no higher than full, nor past the loudest sample", () => {
	expect(gainFactor({ track_gain_db: 4 }, "track")).toBe(1);
	expect(
		gainFactor({ track_gain_db: -1, track_peak: 1.2 }, "track"),
	).toBeCloseTo(1 / 1.2, 3);
});

test("the line sung is the last to have started", () => {
	const lines = [{ start_ms: 1000 }, { start_ms: 4000 }, { start_ms: 9000 }];
	expect(lineAt(lines, 500)).toBe(-1);
	expect(lineAt(lines, 4000)).toBe(1);
	expect(lineAt(lines, 60_000)).toBe(2);
});

test("a kept queue comes back as it was left", () => {
	const items = q("a", "b", "c");
	const kept = keptOf(items, 1, 42, "shuffled", "all", [
		items[2],
		items[0],
		items[1],
	]);
	const back = readKept(JSON.stringify(kept));
	expect(back).toEqual({
		items: items.map(({ key: _, ...i }) => i),
		at: 1,
		position: 42,
		order: "shuffled",
		repeat: "all",
		original: [2, 0, 1],
	});
});

test("what is not a queue kept by this page is none", () => {
	const good = keptOf(q("a"), 0, 0, "in_order", "off", []);
	expect(readKept(null)).toBeUndefined();
	expect(readKept("{")).toBeUndefined();
	expect(readKept(JSON.stringify({ ...good, at: 3 }))).toBeUndefined();
	expect(
		readKept(JSON.stringify({ ...good, items: [{ id: 1 }] })),
	).toBeUndefined();
	expect(
		readKept(JSON.stringify({ ...good, repeat: "sometimes" })),
	).toBeUndefined();
});
