import type { components } from "#lib/api/schema.js";
import type { Choice } from "./subtitles.js";
import { language } from "./words.js";

type Schemas = components["schemas"];

// When subtitles come on unasked, as Jellyfin's subtitle modes: the file's
// own say (its default or forced track), only when the sound is in another
// language, always, only forced ones, or never.
export type SubtitleMode = "default" | "foreign" | "always" | "forced" | "off";
// What the player does at an intro or the credits: offer a button to skip,
// skip by itself, or neither.
export type SkipMode = "button" | "auto" | "off";

// How this browser plays, kept in it: a server keeps no reader's playback
// settings, so each browser has its own, as Plex Web's player settings are.
export type Preferences = {
	// The most the server may send, in kbps; zero is the file as it is.
	quality: number;
	// A language tag; empty is the file's own default track.
	audioLanguage: string;
	// A language tag; empty is the browser's language.
	subtitleLanguage: string;
	subtitleMode: SubtitleMode;
	// Play the next episode when the count after the credits runs out.
	autoplay: boolean;
	skipIntro: SkipMode;
	skipCredits: SkipMode;
};

export const defaults: Preferences = {
	quality: 0,
	audioLanguage: "",
	subtitleLanguage: "",
	subtitleMode: "default",
	autoplay: true,
	skipIntro: "button",
	skipCredits: "button",
};

const key = "photon.playback";

export function loadPreferences(): Preferences {
	try {
		return { ...defaults, ...JSON.parse(localStorage.getItem(key) ?? "{}") };
	} catch {
		return { ...defaults };
	}
}

export function savePreferences(change: Partial<Preferences>): Preferences {
	const next = { ...loadPreferences(), ...change };
	try {
		localStorage.setItem(key, JSON.stringify(next));
	} catch {}
	return next;
}

// Two tags name one language when they read the same: "en", "eng" and
// "en-GB" are all English.
function same(a: string | undefined, b: string | undefined): boolean {
	if (!a || !b) return false;
	const base = (t: string) => language(t.split(/[-_]/)[0]).toLowerCase();
	return base(a) === base(b);
}

// The sound track to ask for, by stream index; undefined leaves it to the
// server, which plays the file's default.
export function pickAudio(
	streams: Schemas["StreamPage"][],
	prefs: Preferences,
): number | undefined {
	const audio = streams.filter((s) => s.kind === "audio");
	const mine = audio.filter((s) => same(s.language, prefs.audioLanguage));
	const main = mine.filter((s) => !s.commentary);
	return (main.find((s) => s.default) ?? main[0] ?? mine[0])?.index;
}

// The subtitle to show unasked: a choice's key, or undefined for none.
export function pickSubtitle(
	choices: Choice[],
	audioLanguage: string | undefined,
	prefs: Preferences,
	browserLanguage: string,
): string | undefined {
	const want = prefs.subtitleLanguage || browserLanguage;
	const mine = choices.filter((c) => same(c.language, want));
	const full = mine.find((c) => !c.forced);
	const forced = mine.find((c) => c.forced) ?? choices.find((c) => c.forced);
	switch (prefs.subtitleMode) {
		case "off":
			return undefined;
		case "forced":
			return forced?.key;
		case "always":
			return (full ?? forced)?.key;
		case "foreign":
			return same(audioLanguage, want) ? forced?.key : (full ?? forced)?.key;
		case "default":
			return (choices.find((c) => c.default) ?? choices.find((c) => c.forced))
				?.key;
	}
}

export function skipping(
	kind: Schemas["MarkerKind"],
	prefs: Preferences,
): SkipMode {
	return kind === "intro" || kind === "recap"
		? prefs.skipIntro
		: prefs.skipCredits;
}
