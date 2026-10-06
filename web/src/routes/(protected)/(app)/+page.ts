import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { railLimit } from "#lib/rows.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.home, keys.userdata);
	// One more than a rail shows, so it knows whether there is more to see.
	return {
		home: await need(
			client(fetch).GET("/api/v1/home", {
				params: { query: { limit: railLimit + 1 } },
			}),
		),
	};
};
