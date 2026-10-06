import { expect, test } from "bun:test";
import { keptChange } from "./preferences.ts";

test("what this browser kept goes up in the server's words", () => {
	expect(
		keptChange({
			quality: 8000,
			audioLanguage: "fr",
			subtitleMode: "foreign",
			autoplay: false,
			skipIntro: "auto",
			skipCredits: "off",
		}),
	).toEqual({
		max_bitrate_kbps: 8000,
		audio_language: "fr",
		audio_track: "language",
		subtitle_language: undefined,
		subtitle_mode: "smart",
		next_episode: "offer",
		intro_action: "skip",
		credits_action: "none",
	});
	// A choice it never made stays the server's.
	expect(keptChange({})).toEqual({
		max_bitrate_kbps: undefined,
		audio_language: undefined,
		audio_track: undefined,
		subtitle_language: undefined,
		subtitle_mode: undefined,
		next_episode: undefined,
		intro_action: undefined,
		credits_action: undefined,
	});
});
