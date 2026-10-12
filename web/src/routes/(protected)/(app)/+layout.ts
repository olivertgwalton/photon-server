import { client, need } from "#lib/api/client.js";
import type { LayoutLoad } from "./$types";

export const load: LayoutLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [libraries, pages, collections] = await Promise.all([
		need(api.GET("/api/v1/libraries")),
		need(api.GET("/api/v1/plugin-pages")),
		// Only whether there is one: the Collections view is offered where it is.
		need(api.GET("/api/v1/collections", { params: { query: { limit: 1 } } })),
	]);
	return {
		libraries: libraries.items,
		collections: collections.total > 0,
		pages: pages.items,
		// The sidebar's own cookie, so it is drawn as it was left.
		sidebarOpen: !document.cookie.includes("sidebar_state=false"),
	};
};
