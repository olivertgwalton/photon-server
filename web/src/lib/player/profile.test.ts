import { expect, test } from "bun:test";
import { browserProfile, type Capabilities } from "./profile.ts";

const t = (codecs: string) => `video/mp4; codecs="${codecs}"`;
const a = (codecs: string) => `audio/mp4; codecs="${codecs}"`;

// What each browser answered on a Mac with an HDR screen, October 2026.
const chrome: Capabilities = {
	types: [
		...["avc1.42E01E", "avc1.4D401E", "avc1.640028", "avc1.640033"].map(t),
		...["avc1.640034", "hvc1.1.6.L120.90", "hvc1.1.6.L153.90"].map(t),
		...["hvc1.1.6.L183.90", "hvc1.2.4.L153.90", "av01.0.08M.08"].map(t),
		...["av01.0.08M.10", "vp09.00.40.08", "vp09.02.40.10"].map(t),
		...["mp4a.40.2", "opus", "flac"].map(a),
		"audio/mpeg",
		'audio/webm; codecs="vorbis"',
		"video/mp4",
		"video/webm",
		"video/x-matroska",
	],
	pq: true,
	hlg: true,
	nativeHLS: false,
};

const safari: Capabilities = {
	types: [
		...["avc1.42E01E", "avc1.4D401E", "avc1.640028", "avc1.640033"].map(t),
		...["avc1.640034", "hvc1.1.6.L120.90", "hvc1.1.6.L153.90"].map(t),
		...["hvc1.1.6.L183.90", "hvc1.2.4.L153.90", "dvh1.05.06"].map(t),
		...["dvh1.08.06", "vp09.00.40.08", "vp09.02.40.10"].map(t),
		...["mp4a.40.2", "ac-3", "ec-3", "alac", "flac", "opus"].map(a),
		"audio/mpeg",
		"video/mp4",
		"video/webm",
	],
	pq: true,
	hlg: true,
	nativeHLS: true,
};

const firefox: Capabilities = {
	types: [
		...["avc1.42E01E", "avc1.4D401E", "avc1.640028", "avc1.640033"].map(t),
		...["avc1.640034", "av01.0.08M.08", "av01.0.08M.10"].map(t),
		...["vp09.00.40.08", "vp09.02.40.10"].map(t),
		...["mp4a.40.2", "opus", "flac"].map(a),
		"audio/mpeg",
		'audio/webm; codecs="vorbis"',
		"video/mp4",
		"video/webm",
	],
	pq: false,
	hlg: false,
	nativeHLS: false,
};

const codecs = (caps: Capabilities) =>
	browserProfile(caps, 0).video.map((v) => v.codec);

test("chrome opens matroska as it is and shows HDR10 HEVC", () => {
	const profile = browserProfile(chrome, 0);
	expect(profile.containers).toEqual(["mp4", "matroska", "webm"]);
	expect(codecs(chrome)).toEqual(["h264", "hevc", "av1", "vp9"]);
	const h265 = profile.video.find((v) => v.codec === "hevc");
	expect(h265?.profiles).toContain("Main 10");
	expect(h265?.max_level).toBe(183);
	expect(h265?.ranges).toEqual(["sdr", "hdr10", "hdr10plus", "hlg"]);
	expect(h265?.dolby_vision_profiles).toBeUndefined();
});

test("chrome is sent no Dolby audio, which it cannot decode", () => {
	const audio = browserProfile(chrome, 0).audio.map((x) => x.codec);
	expect(audio).not.toContain("ac3");
	expect(audio).not.toContain("eac3");
	expect(audio[0]).toBe("aac");
});

test("safari takes Dolby Vision and Dolby audio but no matroska", () => {
	const profile = browserProfile(safari, 0);
	expect(profile.containers).toEqual(["mp4"]);
	const h265 = profile.video.find((v) => v.codec === "hevc");
	expect(h265?.ranges).toContain("dv");
	expect(h265?.dolby_vision_profiles).toEqual([5, 8]);
	expect(profile.audio.map((x) => x.codec)).toEqual(
		expect.arrayContaining(["ac3", "eac3", "alac"]),
	);
});

test("firefox, opening WebM but not Matroska, is sent neither as it is", () => {
	const profile = browserProfile(firefox, 0);
	expect(profile.containers).toEqual(["mp4"]);
	expect(codecs(firefox)).toEqual(["h264", "av1", "vp9"]);
});

test("an SDR screen is sent HDR toned down", () => {
	const profile = browserProfile(firefox, 0);
	for (const v of profile.video) expect(v.ranges ?? ["sdr"]).toEqual(["sdr"]);
});

test("H.264 is eight-bit and as high a level as the browser said", () => {
	const h264 = browserProfile(firefox, 0).video[0];
	expect(h264).toMatchObject({
		codec: "h264",
		max_level: 52,
		max_bit_depth: 8,
	});
	expect(h264?.profiles).toContain("High");
});

test("the quality chosen is the most the server sends", () => {
	expect(browserProfile(chrome, 4000).max_bitrate_kbps).toBe(4000);
	expect(browserProfile(chrome, 0).max_bitrate_kbps).toBe(0);
});

test("a browser draws only plain text files it is given", () => {
	expect(browserProfile(safari, 0).subtitles).toEqual([
		{ codec: "subrip", delivery: "sidecar" },
		{ codec: "webvtt", delivery: "sidecar" },
	]);
});
