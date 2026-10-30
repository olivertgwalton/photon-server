import { client, need } from "#lib/api/client.ts";
import type { LayoutLoad } from "./$types";

export const load: LayoutLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [libraries, pages, collections, requests] = await Promise.all([
		need(api.GET("/api/v1/libraries")),
		need(api.GET("/api/v1/plugin-pages")),
		// Only whether there is one: the Collections view is offered where it is,
		// and Requests once the profile has asked for something.
		need(api.GET("/api/v1/collections", { params: { query: { limit: 1 } } })),
		need(api.GET("/api/v1/requests", { params: { query: { limit: 1 } } })),
	]);
	return {
		libraries: libraries.items,
		collections: collections.items.length > 0,
		requests: requests.items.length > 0,
		pages: pages.items,
		// The sidebar's own cookie, so it is drawn as it was left.
		sidebarOpen: !document.cookie.includes("sidebar_state=false"),
	};
};
