import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { LayoutLoad } from "./$types";

export const load: LayoutLoad = async ({ fetch, params, parent, depends }) => {
	depends(keys.library(params.id));
	const [{ libraries }, facets] = await Promise.all([
		parent(),
		need(
			client(fetch).GET("/api/v1/libraries/{id}/facets", {
				params: { path: { id: params.id } },
			}),
		),
	]);
	const library = libraries.find((l) => l.id === params.id);
	if (!library) error(404, "That library isn't here any more.");
	return { library, facets };
};
