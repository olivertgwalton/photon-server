import type { components } from "#lib/api/schema.d.ts";

type Schemas = components["schemas"];

export const skips: Record<Schemas["MarkerKind"], string> = {
	intro: "Skip Intro",
	recap: "Skip Recap",
	credits: "Skip Credits",
	preview: "Skip Preview",
};

export const speeds = [0.5, 0.75, 1, 1.25, 1.5, 2];

// How the picture meets a screen of another shape: whole, cropped to fill it,
// or stretched to it.
export const fits = {
	contain: { label: "Fit", class: "object-contain" },
	cover: { label: "Crop to fill", class: "object-cover" },
	fill: { label: "Stretch", class: "object-fill" },
};

export type Fit = keyof typeof fits;

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
