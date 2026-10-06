import { expect, test } from "bun:test";
import type { components } from "#lib/api/schema.js";
import { defaults, pickAudio, pickSubtitle, skipping } from "./preferences.ts";
import type { Choice } from "./subtitles.ts";

type Stream = components["schemas"]["StreamPage"];

const audio = (index: number, language: string, more: Partial<Stream> = {}) =>
	({ index, kind: "audio", codec: "aac", language, ...more }) as Stream;

test("the sound in the reader's language is asked for, else the file's own", () => {
	const streams = [
		audio(1, "jpn", { default: true }),
		audio(2, "eng", { commentary: true }),
		audio(3, "eng"),
	];
	// Never the commentary, while there is the film's own sound.
	expect(pickAudio(streams, { ...defaults, audioLanguage: "en" })).toBe(3);
	expect(
		pickAudio(
			[
				audio(1, "jpn"),
				audio(2, "eng", { commentary: true }),
				audio(3, "eng", { default: true }),
			],
			{ ...defaults, audioLanguage: "en" },
		),
	).toBe(3);
	expect(pickAudio(streams, defaults)).toBeUndefined();
	expect(
		pickAudio(streams, { ...defaults, audioLanguage: "fr" }),
	).toBeUndefined();
});

const subs: Choice[] = [
	{ key: "s3", label: "", codec: "subrip", language: "eng", forced: true },
	{ key: "s4", label: "", codec: "subrip", language: "eng" },
	{ key: "s5", label: "", codec: "subrip", language: "fre", default: true },
];

test("subtitles come on as the mode says", () => {
	const pick = (mode: (typeof defaults)["subtitleMode"], sound: string) =>
		pickSubtitle(subs, sound, { ...defaults, subtitleMode: mode }, "en-GB");
	expect(pick("default", "eng")).toBe("s5");
	expect(pick("off", "jpn")).toBeUndefined();
	expect(pick("always", "eng")).toBe("s4");
	expect(pick("forced", "jpn")).toBe("s3");
	// A film in the reader's own language shows only what is forced.
	expect(pick("foreign", "eng")).toBe("s3");
	expect(pick("foreign", "jpn")).toBe("s4");
});

test("intros and recaps follow one choice, credits and previews the other", () => {
	const prefs = { ...defaults, skipIntro: "auto", skipCredits: "off" } as const;
	expect(skipping("recap", prefs)).toBe("auto");
	expect(skipping("preview", prefs)).toBe("off");
});
