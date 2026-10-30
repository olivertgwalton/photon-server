import { client, need } from "#lib/api/client.ts";
import { wallPageSize } from "#lib/wall.ts";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => ({
	collections: await need(
		client(fetch).GET("/api/v1/collections", {
			params: { query: { limit: wallPageSize, total: "exact" } },
		}),
	),
});
