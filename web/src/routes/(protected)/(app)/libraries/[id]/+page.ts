import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import {
	type ViewStyle,
	viewKey,
	viewStyles,
	wallPageSize,
	wallQuery,
} from "#lib/wall.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = ({ fetch, params, url, depends }) => {
	depends(keys.library(params.id), keys.userdata);
	const api = client(fetch);
	const query = wallQuery(url.searchParams);
	const { sort, order, ...filters } = query;
	const path = { id: params.id };
	// Letters are where the wall is in title order; any other order has none.
	// Streamed, not awaited: the page is drawn at once, the wall as it comes,
	// saying which query it answers.
	const wall = Promise.all([
		need(
			api.GET("/api/v1/libraries/{id}/titles", {
				params: { path, query: { ...query, limit: wallPageSize } },
			}),
		),
		(sort ?? "title") === "title"
			? need(
					api.GET("/api/v1/libraries/{id}/letters", {
						params: { path, query: filters },
					}),
				)
			: undefined,
	]).then(([titles, letters]) => ({ query, titles, letters: letters?.items }));
	// The page takes the refusal; a load preloaded and never shown has no page.
	wall.catch(() => {});
	return {
		query,
		// A smart collection whose rule is being changed here.
		editing: url.searchParams.get("collection") ?? undefined,
		wall,
		view: storedView(params.id),
	};
};

// How the reader last drew this library, kept by this browser.
function storedView(library: string): ViewStyle {
	try {
		const view = localStorage.getItem(viewKey(library)) as ViewStyle;
		return viewStyles.includes(view) ? view : "poster";
	} catch {
		return "poster";
	}
}
