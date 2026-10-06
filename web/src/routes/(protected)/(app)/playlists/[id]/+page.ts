import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

// The most entries one request answers.
const pageLimit = 200;

export const load: PageLoad = async ({ fetch, params, depends }) => {
	depends(keys.userdata, "photon:playlists");
	const api = client(fetch);
	const path = { id: params.id };
	const entries = (offset: number) =>
		need(
			api.GET("/api/v1/playlists/{id}/entries", {
				params: { path, query: { offset, limit: pageLimit } },
			}),
		);
	const [lists, first] = await Promise.all([
		need(api.GET("/api/v1/playlists")),
		entries(0),
	]);
	const playlist = lists.items.find((p) => p.id === params.id);
	if (!playlist) error(404, "That playlist isn't here any more.");
	// A list is reordered as a whole, so all of it is drawn.
	const rest = await Promise.all(
		Array.from({ length: Math.ceil(first.total / pageLimit) - 1 }, (_, i) =>
			entries((i + 1) * pageLimit),
		),
	);
	return {
		playlist,
		entries: [first, ...rest].flatMap((page) => page.items),
	};
};
