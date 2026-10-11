import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { homeRails } from "#lib/home.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, parent, depends }) => {
	depends(keys.home, keys.userdata);
	const [{ libraries }, prefs] = await Promise.all([
		parent(),
		need(client(fetch).GET("/api/v1/profile/preferences")),
	]);
	// Streamed, not awaited: the page is drawn at once, each row as it comes.
	const rails = homeRails(fetch, prefs.home, libraries);
	// The page takes a refusal; a load preloaded and never shown has no page.
	for (const r of rails) r.cards.catch(() => {});
	return { rails };
};
