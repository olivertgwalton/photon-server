import { expect, test } from "bun:test";
import {
	losslessCodec,
	mediaStart,
	segmentAt,
	segmentsOf,
	trackAt,
} from "./joined.ts";

test("only sound copied without loss joins", () => {
	expect(
		losslessCodec('#EXT-X-STREAM-INF:BANDWIDTH=1,CODECS="fLaC"\nvideo.m3u8'),
	).toBe("flac");
	expect(
		losslessCodec('#EXT-X-STREAM-INF:BANDWIDTH=1,CODECS="alac"\nvideo.m3u8'),
	).toBe("alac");
	expect(
		losslessCodec(
			'#EXT-X-STREAM-INF:BANDWIDTH=1,CODECS="mp4a.40.2"\nvideo.m3u8',
		),
	).toBeUndefined();
});

test("a playlist's segments are read beside it, each where it starts", () => {
	const media = `#EXTM3U
#EXT-X-MAP:URI="init0.mp4"
#EXTINF:6.000000,
0.m4s
#EXTINF:3.000000,
1.m4s
#EXT-X-ENDLIST`;
	expect(
		segmentsOf(media, "http://photon/api/v1/hls/p/1/s/video.m3u8"),
	).toEqual({
		init: "http://photon/api/v1/hls/p/1/s/init0.mp4",
		segments: [
			{ url: "http://photon/api/v1/hls/p/1/s/0.m4s", at: 0 },
			{ url: "http://photon/api/v1/hls/p/1/s/1.m4s", at: 6 },
		],
		length: 9,
	});
});

test("a seek feeds from the segment it falls in", () => {
	const segments = [
		{ url: "0", at: 0 },
		{ url: "1", at: 6 },
		{ url: "2", at: 12 },
	];
	expect(segmentAt(segments, 0)).toBe(0);
	expect(segmentAt(segments, 7.5)).toBe(1);
	expect(segmentAt(segments, 290)).toBe(2);
});

test("the track playing is the last to have started", () => {
	const tracks = [
		{ start: 0 },
		{ start: 308.5 },
		{ start: Number.POSITIVE_INFINITY },
	];
	expect(trackAt(tracks, 10)).toBe(0);
	expect(trackAt(tracks, 308.5)).toBe(1);
	expect(trackAt(tracks, 600)).toBe(1);
});

// A box of a type, its version, and its payload after its flags.
function box(type: string, version: number, payload: number[]): number[] {
	const size = 12 + payload.length;
	return [
		0,
		0,
		0,
		size,
		...[...type].map((c) => c.charCodeAt(0)),
		version,
		0,
		0,
		0,
		...payload,
	];
}
const u32 = (n: number) => [
	n >>> 24,
	(n >>> 16) & 255,
	(n >>> 8) & 255,
	n & 255,
];

test("a track's own timeline starts at its first segment's decode time", () => {
	// mdhd v0: creation, modification, then the timescale.
	const init = box("mdhd", 0, [...u32(0), ...u32(0), ...u32(44100), ...u32(0)]);
	// tfdt v0: the decode time; v1 the same in 64 bits.
	const v0 = box("tfdt", 0, u32(441000));
	const v1 = box("tfdt", 1, [...u32(0), ...u32(441000)]);
	const buf = (bytes: number[]) => new Uint8Array(bytes).buffer;
	expect(mediaStart(buf(init), buf(v0))).toBe(10);
	expect(mediaStart(buf(init), buf(v1))).toBe(10);
	expect(mediaStart(buf([]), buf(v0))).toBe(0);
});
