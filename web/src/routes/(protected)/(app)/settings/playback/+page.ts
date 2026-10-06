import { loadPreferences } from "#lib/player/load.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => ({
	prefs: await loadPreferences(fetch),
});
