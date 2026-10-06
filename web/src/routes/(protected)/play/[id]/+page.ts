import { client, need } from "#lib/api/client.js";
import { keptHere, loadPreferences } from "#lib/player/load.js";
import type { PageLoad } from "./$types";

// The player asks the browser what it plays, so it runs there alone.
export const ssr = false;

// What the player needs before it asks to play: the copies, their tracks,
// chapters, markers and thumbnails, where the reader left off, and how the
// profile plays.
export const load: PageLoad = async ({ fetch, params }) => {
	const api = client(fetch);
	const preferences = loadPreferences(fetch);
	// Settings this browser kept change the tracks the server chooses once they
	// are the profile's, so the title waits for them to go up.
	if (keptHere()) await preferences;
	const [title, prefs] = await Promise.all([
		need(
			api.GET("/api/v1/titles/{id}", { params: { path: { id: params.id } } }),
		),
		preferences,
	]);
	return { title, prefs };
};
