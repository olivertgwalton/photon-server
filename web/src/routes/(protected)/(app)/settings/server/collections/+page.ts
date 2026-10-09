import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.admin.libraries);
	const api = client(fetch);
	const [{ items }, plugins] = await Promise.all([
		need(api.GET("/api/v1/admin/libraries")),
		need(api.GET("/api/v1/admin/plugins")),
	]);
	const shelves = await Promise.all(
		items.map(async (library) => ({
			library,
			// The server's count is everything, so none there is none to list.
			collections: library.counts.collections
				? (
						await need(
							api.GET("/api/v1/libraries/{id}/collections", {
								params: { path: { id: library.id }, query: { limit: 200 } },
							}),
						)
					).items
				: [],
		})),
	);
	// A plugin that keeps lists, as a Stremio addon's catalogs are, is a source of them too.
	const listers = plugins.items.filter((p) => p.capabilities.includes("list"));
	return { shelves, listers };
};
