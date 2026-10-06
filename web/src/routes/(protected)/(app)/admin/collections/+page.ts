import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	const api = client(fetch);
	const { items } = await need(api.GET("/api/v1/admin/libraries"));
	const shelves = await Promise.all(
		items.map(async (library) => ({
			library,
			collections: (
				await need(
					api.GET("/api/v1/libraries/{id}/collections", {
						params: { path: { id: library.id }, query: { limit: 200 } },
					}),
				)
			).items,
		})),
	);
	return { shelves };
};
