import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

// Every favourite the server will list at once: its home row at its longest.
export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.home, keys.userdata);
	const home = await need(
		client(fetch).GET("/api/v1/home", { params: { query: { limit: 200 } } }),
	);
	return {
		cards: home.rows.find((r) => r.kind === "favourites")?.items ?? [],
	};
};
