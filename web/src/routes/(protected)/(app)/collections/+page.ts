import { client, need } from "#lib/api/client.js";
import { wallPageSize } from "#lib/wall.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => ({
	collections: await need(
		client(fetch).GET("/api/v1/collections", {
			params: { query: { limit: wallPageSize } },
		}),
	),
});
