import type { components } from "#lib/api/schema.js";
import { language } from "#lib/format.js";

type Schemas = components["schemas"];

export const reasons: Record<Schemas["TranscodeReason"], string> = {
	container_not_supported: "This browser doesn't open the file's container.",
	video_codec_not_supported: "This browser doesn't play the video's codec.",
	video_profile_not_supported: "This browser doesn't play the video's profile.",
	video_level_not_supported:
		"The video's level is higher than this browser plays.",
	video_resolution_not_supported:
		"The picture is larger than this browser plays.",
	video_bit_depth_not_supported:
		"The video's bit depth is more than this browser plays.",
	video_range_not_supported: "This screen doesn't show the video's HDR.",
	audio_codec_not_supported: "This browser doesn't play the audio's codec.",
	audio_channels_not_supported:
		"The audio has more channels than this browser plays.",
	bitrate_exceeds_limit: "The file is above the quality chosen.",
	subtitle_codec_not_supported:
		"The subtitles are pictures, drawn into the video.",
	parts_not_supported: "The title is in several files, played as one.",
};

export const methods: Record<Schemas["PlayMethod"], string> = {
	direct: "Direct play",
	remux: "Remux",
	transcode: "Transcode",
};

export const skips: Record<Schemas["MarkerKind"], string> = {
	intro: "Skip Intro",
	recap: "Skip Recap",
	credits: "Skip Credits",
	preview: "Skip Preview",
};

// The most the server may send, as Jellyfin's quality menu offers it; zero
// is the file as it is.
export const qualities = [
	0, 40_000, 20_000, 10_000, 8_000, 6_000, 4_000, 3_000, 2_000, 1_500, 720, 420,
];

// The languages subtitles are most often wanted in, offered wherever they are
// chosen, by BCP 47 tag.
export const subtitleLanguages = [
	"en",
	"es",
	"fr",
	"de",
	"it",
	"pt",
	"pt-BR",
	"nl",
	"sv",
	"da",
	"no",
	"fi",
	"pl",
	"cs",
	"hu",
	"ro",
	"el",
	"tr",
	"ru",
	"uk",
	"ar",
	"he",
	"hi",
	"ja",
	"ko",
	"zh-CN",
	"zh-TW",
];

const layouts: Record<number, string> = {
	1: "Mono",
	2: "Stereo",
	6: "5.1",
	8: "7.1",
};

export function channels(n: number | undefined): string {
	return n ? (layouts[n] ?? `${n} ch`) : "";
}

export function audioLabel(s: Schemas["StreamPage"]): string {
	return [
		s.title || language(s.language) || "Unknown",
		s.codec.toUpperCase(),
		channels(s.channels),
		s.commentary ? "Commentary" : "",
	]
		.filter(Boolean)
		.join(" · ");
}
