import { client, need } from "#lib/api/client.ts";
import { keys } from "#lib/changes.ts";
import type { PageLoad } from "./$types";

// Every favourite the server will list at once, its music and books among
// them: Home's row is its films and shows alone.
export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.home, keys.userdata);
	const row = await need(
		client(fetch).GET("/api/v1/profile/favourites", {
			params: { query: { limit: 200 } },
		}),
	);
	return { cards: row.items };
};
