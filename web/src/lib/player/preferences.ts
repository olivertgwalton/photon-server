import type { components } from "#lib/api/schema.js";

type Schemas = components["schemas"];
export type Preferences = Schemas["Preferences"];
export type PreferencesChange = Schemas["PreferencesChange"];

// Where this browser kept them before the server did.
export const kept = "photon.playback";

type Kept = {
	quality?: number;
	audioLanguage?: string;
	subtitleLanguage?: string;
	subtitleMode?: "default" | "foreign" | "always" | "forced" | "off";
	autoplay?: boolean;
	skipIntro?: "button" | "auto" | "off";
	skipCredits?: "button" | "auto" | "off";
};

const modes = {
	default: "default",
	foreign: "smart",
	always: "always",
	forced: "only_forced",
	off: "none",
} as const;
const actions = { button: "ask", auto: "skip", off: "none" } as const;

// What this browser kept, as a change to the server's.
export function keptChange(k: Kept): PreferencesChange {
	return {
		max_bitrate_kbps: k.quality,
		audio_language: k.audioLanguage,
		subtitle_language: k.subtitleLanguage,
		subtitle_mode: k.subtitleMode && modes[k.subtitleMode],
		next_episode:
			k.autoplay === undefined ? undefined : k.autoplay ? "play" : "offer",
		intro_action: k.skipIntro && actions[k.skipIntro],
		credits_action: k.skipCredits && actions[k.skipCredits],
		// A language kept here was asked for by name, so it is that track.
		audio_track: k.audioLanguage ? "language" : undefined,
	};
}

export function skipping(
	kind: Schemas["MarkerKind"],
	prefs: Preferences,
): Schemas["SegmentAction"] {
	return kind === "intro" || kind === "recap"
		? prefs.intro_action
		: prefs.credits_action;
}
