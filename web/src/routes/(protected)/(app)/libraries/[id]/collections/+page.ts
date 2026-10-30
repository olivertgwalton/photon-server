import { client, need } from "#lib/api/client.ts";
import { wallPageSize } from "#lib/wall.ts";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params }) => ({
	collections: await need(
		client(fetch).GET("/api/v1/libraries/{id}/collections", {
			params: {
				path: { id: params.id },
				query: { limit: wallPageSize, total: "exact" },
			},
		}),
	),
});
