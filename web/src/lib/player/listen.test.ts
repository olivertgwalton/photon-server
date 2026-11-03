import { expect, test } from "bun:test";
import {
	type AudioCapabilities,
	audioProfile,
	nextAt,
	previousAt,
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
