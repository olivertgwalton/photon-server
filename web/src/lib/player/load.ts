import { client, need } from "#lib/api/client.js";
import {
	kept,
	keptChange,
	type Preferences,
	type PreferencesChange,
} from "./preferences.js";

// What this browser kept before the server kept them, if it still has it.
export function keptHere(): string | null {
	try {
		return localStorage.getItem(kept);
	} catch {
		return null;
	}
}

// How the profile plays. What this browser kept before the server kept them
// goes up once, where the profile has never changed them.
export async function loadPreferences(
	fetch: typeof globalThis.fetch = globalThis.fetch,
): Promise<Preferences> {
	const api = client(fetch);
	const data = await need(api.GET("/api/v1/me/preferences"));
	const local = keptHere();
	if (!local) return data;
	if (data.saved_at) {
		localStorage.removeItem(kept);
		return data;
	}
	let change: PreferencesChange;
	try {
		change = keptChange(JSON.parse(local));
	} catch {
		localStorage.removeItem(kept);
		return data;
	}
	const { data: moved } = await api.PATCH("/api/v1/me/preferences", {
		body: change,
	});
	if (!moved) return data;
	localStorage.removeItem(kept);
	return moved;
}
