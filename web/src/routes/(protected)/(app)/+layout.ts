import { client, need } from "#lib/api/client.js";
import type { LayoutLoad } from "./$types";

export const load: LayoutLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [{ items }, words] = await Promise.all([
		need(api.GET("/api/v1/libraries")),
		need(api.GET("/api/v1/words")),
	]);
	return {
		libraries: items,
		words,
		// The sidebar's own cookie, so it is drawn as it was left.
		sidebarOpen: !document.cookie.includes("sidebar_state=false"),
	};
};
