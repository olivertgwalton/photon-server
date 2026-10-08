import type { components } from "#lib/api/schema.js";

type Schemas = components["schemas"];

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
