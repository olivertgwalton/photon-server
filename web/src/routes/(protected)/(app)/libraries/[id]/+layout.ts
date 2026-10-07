import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { LayoutLoad } from "./$types";

export const load: LayoutLoad = async ({ fetch, params, parent, depends }) => {
	depends(keys.library(params.id));
	const api = client(fetch);
	const path = { id: params.id };
	const [{ libraries }, facets, collections] = await Promise.all([
		parent(),
		need(api.GET("/api/v1/libraries/{id}/facets", { params: { path } })),
		// Only its total is wanted: whether to offer the tab at all.
		need(
			api.GET("/api/v1/libraries/{id}/collections", {
				params: { path, query: { limit: 1 } },
			}),
		),
	]);
	const library = libraries.find((l) => l.id === params.id);
	if (!library) error(404, "That library isn't here any more.");
	return { library, facets, collections: collections.total };
};
