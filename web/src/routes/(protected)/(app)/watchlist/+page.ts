import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { wallPageSize } from "#lib/wall.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.userdata);
	return {
		watchlist: await need(
			client(fetch).GET("/api/v1/home/{row}", {
				params: {
					path: { row: "watchlist" },
					query: { limit: wallPageSize },
				},
			}),
		),
	};
};
