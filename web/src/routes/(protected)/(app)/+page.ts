import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { railLimit } from "#lib/rows.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = ({ fetch, depends }) => {
	depends(keys.home, keys.userdata);
	// One more than a rail shows, so it knows whether there is more to see.
	// Streamed, not awaited: the page is drawn at once, its rows as they come.
	const home = need(
		client(fetch).GET("/api/v1/home", {
			params: { query: { limit: railLimit + 1 } },
		}),
	);
	// The page takes the refusal; a load preloaded and never shown has no page.
	home.catch(() => {});
	return { home };
};
