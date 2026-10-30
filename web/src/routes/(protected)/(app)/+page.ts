import { client, need } from "#lib/api/client.ts";
import { keys } from "#lib/changes.ts";
import { homeRails } from "#lib/home.ts";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, parent, depends }) => {
	depends(keys.home, keys.userdata);
	const [{ libraries }, prefs] = await Promise.all([
		parent(),
		need(client(fetch).GET("/api/v1/profile/preferences")),
	]);
	// Streamed, not awaited: the page is drawn at once, each row as its turn
	// comes. A row waits for the rows above it to have answered, so one that is
	// slow appends to the page rather than appearing above rows already drawn
	// and pushing them down.
	const rails = homeRails(fetch, prefs.home, libraries);
	let above: Promise<unknown> = Promise.resolve();
	for (const r of rails) {
		const cards = r.cards;
		r.cards = above.then(() => cards);
		// The page takes a refusal; a load preloaded and never shown has no page.
		above = r.cards.catch(() => {});
	}
	return { rails };
};
