import { client, need } from "#lib/api/client.js";
import type { LayoutLoad } from "./$types";

export const load: LayoutLoad = async ({ fetch }) => {
	const { items } = await need(client(fetch).GET("/api/v1/libraries"));
	return {
		libraries: items,
		// The sidebar's own cookie, so it is drawn as it was left.
		sidebarOpen: !document.cookie.includes("sidebar_state=false"),
	};
};
