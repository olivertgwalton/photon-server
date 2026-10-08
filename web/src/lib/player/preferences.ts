import type { components } from "#lib/api/schema.js";

type Schemas = components["schemas"];
export type Preferences = Schemas["Preferences"];
export type PreferencesChange = Schemas["PreferencesChange"];

export function skipping(
	kind: Schemas["MarkerKind"],
	prefs: Preferences,
): Schemas["SegmentAction"] {
	return kind === "intro" || kind === "recap"
		? prefs.intro_action
		: prefs.credits_action;
}
