import type { components } from "#lib/api/schema.js";

type Schemas = components["schemas"];
type ClientProfile = Schemas["ClientProfile"];

// What a browser says it plays, as asked: the types its video element and
// media sources take, whether it shows HDR, and whether it plays HLS itself.
export type Capabilities = {
	types: string[];
	pq: boolean;
	hlg: boolean;
	nativeHLS: boolean;
};

// Each question is a type a browser is asked about, and what a yes means in
// the server's words: ffprobe's codec, profile and level names.
const h264 = {
	baseline: 'video/mp4; codecs="avc1.42E01E"',
	main: 'video/mp4; codecs="avc1.4D401E"',
	high: 'video/mp4; codecs="avc1.640028"',
	level51: 'video/mp4; codecs="avc1.640033"',
	level52: 'video/mp4; codecs="avc1.640034"',
};
const hevc = {
	main: 'video/mp4; codecs="hvc1.1.6.L120.90"',
	level51: 'video/mp4; codecs="hvc1.1.6.L153.90"',
	level61: 'video/mp4; codecs="hvc1.1.6.L183.90"',
	main10: 'video/mp4; codecs="hvc1.2.4.L153.90"',
	dv5: 'video/mp4; codecs="dvh1.05.06"',
	dv8: 'video/mp4; codecs="dvh1.08.06"',
};
const av1 = {
	main: 'video/mp4; codecs="av01.0.08M.08"',
	main10: 'video/mp4; codecs="av01.0.08M.10"',
};
const vp9 = {
	profile0: 'video/mp4; codecs="vp09.00.40.08"',
	profile2: 'video/mp4; codecs="vp09.02.40.10"',
};
const audio = {
	aac: 'audio/mp4; codecs="mp4a.40.2"',
	mp3: "audio/mpeg",
	opus: 'audio/mp4; codecs="opus"',
	flac: 'audio/mp4; codecs="flac"',
	ac3: 'audio/mp4; codecs="ac-3"',
	eac3: 'audio/mp4; codecs="ec-3"',
	alac: 'audio/mp4; codecs="alac"',
	vorbis: 'audio/webm; codecs="vorbis"',
};
const containers = {
	mp4: "video/mp4",
	webm: "video/webm",
	matroska: "video/x-matroska",
};

const questions = [
	...Object.values(h264),
	...Object.values(hevc),
	...Object.values(av1),
	...Object.values(vp9),
	...Object.values(audio),
	...Object.values(containers),
];

// Surround is sent where the browser decodes it; it mixes it down for the
// speakers it has, as Jellyfin's web profile assumes.
const surround = 6;

// The profile sent with a play, built from what the browser answered, as
// Jellyfin's browserDeviceProfile builds its own. A browser draws no subtitle
// from inside a file, so a picture subtitle is drawn into the video for it.
export function browserProfile(
	caps: Capabilities,
	maxBitrateKbps: number,
): ClientProfile {
	const has = (type: string) => caps.types.includes(type);
	const hdr: Schemas["Range"][] = [];
	if (caps.pq) hdr.push("hdr10", "hdr10plus");
	if (caps.hlg) hdr.push("hlg");
	const video: Schemas["VideoSupport"][] = [];

	if (has(h264.baseline) || has(h264.high)) {
		video.push({
			codec: "h264",
			profiles: [
				"Constrained Baseline",
				"Baseline",
				...(has(h264.main) || has(h264.high) ? ["Main"] : []),
				...(has(h264.high) ? ["High"] : []),
			],
			max_level: has(h264.level52) ? 52 : has(h264.level51) ? 51 : 42,
			max_bit_depth: 8,
		});
	}
	if (has(hevc.main)) {
		const dolbyVision = [has(hevc.dv5) && 5, has(hevc.dv8) && 8].filter(
			(p) => p !== false,
		);
		video.push({
			codec: "hevc",
			profiles: ["Main", ...(has(hevc.main10) ? ["Main 10"] : [])],
			max_level: has(hevc.level61) ? 183 : has(hevc.level51) ? 153 : 120,
			max_bit_depth: has(hevc.main10) ? 10 : 8,
			ranges: [
				"sdr",
				...(has(hevc.main10) ? hdr : []),
				...(dolbyVision.length ? (["dv"] as const) : []),
			],
			...(dolbyVision.length ? { dolby_vision_profiles: dolbyVision } : {}),
		});
	}
	if (has(av1.main)) {
		video.push({
			codec: "av1",
			profiles: ["Main"],
			max_bit_depth: has(av1.main10) ? 10 : 8,
			ranges: ["sdr", ...(has(av1.main10) ? hdr : [])],
		});
	}
	if (has(vp9.profile0)) {
		video.push({
			codec: "vp9",
			profiles: ["Profile 0", ...(has(vp9.profile2) ? ["Profile 2"] : [])],
			max_bit_depth: has(vp9.profile2) ? 10 : 8,
			ranges: ["sdr", ...(has(vp9.profile2) ? hdr : [])],
		});
	}

	const sound: Schemas["AudioSupport"][] = [];
	// Order is preference: the server encodes to the first it can.
	if (has(audio.aac)) sound.push({ codec: "aac", max_channels: surround });
	if (has(audio.eac3)) sound.push({ codec: "eac3", max_channels: surround });
	if (has(audio.ac3)) sound.push({ codec: "ac3", max_channels: surround });
	if (has(audio.opus)) sound.push({ codec: "opus", max_channels: surround });
	if (has(audio.flac)) sound.push({ codec: "flac", max_channels: surround });
	if (has(audio.alac)) sound.push({ codec: "alac", max_channels: surround });
	if (has(audio.mp3)) sound.push({ codec: "mp3", max_channels: 2 });
	if (has(audio.vorbis)) sound.push({ codec: "vorbis", max_channels: 2 });

	const opens: string[] = [];
	if (has(containers.mp4)) opens.push("mp4");
	// ffprobe names Matroska and WebM alike ("matroska,webm"), so a browser
	// that opens only WebM must not be sent a Matroska file as it is.
	if (has(containers.matroska) && has(containers.webm))
		opens.push("matroska", "webm");

	return {
		containers: opens,
		video,
		audio: sound,
		max_bitrate_kbps: maxBitrateKbps,
		// A video element draws only a WebVTT track it is given, which SubRip
		// becomes; pictures and styled text are drawn into the video.
		subtitles: [
			{ codec: "subrip", delivery: "sidecar" },
			{ codec: "webvtt", delivery: "sidecar" },
		],
	};
}

// What this browser answers. A type counts where the video element plays it
// and, unless the browser plays HLS itself, where hls.js can feed it to the
// element through media sources too.
export async function probe(): Promise<Capabilities> {
	const element = document.createElement("video");
	const appleHLS =
		navigator.vendor.startsWith("Apple") &&
		element.canPlayType("application/vnd.apple.mpegurl") !== "";
	const nativeHLS = appleHLS || !("MediaSource" in window);
	const plays = (type: string) =>
		element.canPlayType(type) !== "" &&
		(nativeHLS ||
			!type.includes("codecs") ||
			MediaSource.isTypeSupported(type));
	const types = questions.filter(plays);
	// HDR is asked of a ten-bit codec the browser has, and of the screen.
	const tenBit = [hevc.main10, av1.main10, vp9.profile2].find((t) =>
		types.includes(t),
	);
	const shows = async (transferFunction: "pq" | "hlg") => {
		if (!tenBit || !matchMedia("(dynamic-range: high)").matches) return false;
		const info = await navigator.mediaCapabilities
			.decodingInfo({
				type: "file",
				video: {
					contentType: tenBit,
					width: 3840,
					height: 2160,
					bitrate: 20_000_000,
					framerate: 24,
					transferFunction,
					colorGamut: "rec2020",
				},
			})
			.catch(() => null);
		return info?.supported ?? false;
	};
	const [pq, hlg] = await Promise.all([shows("pq"), shows("hlg")]);
	return { types, pq, hlg, nativeHLS };
}
