import { need } from "#lib/server/api.js";
import type { LayoutServerLoad } from "./$types";

export const load: LayoutServerLoad = async ({ locals, cookies }) => {
	const { items } = await need(locals.api.GET("/api/v1/libraries"));
	return {
		libraries: items,
		// The sidebar's own cookie, so it is drawn as it was left.
		sidebarOpen: cookies.get("sidebar_state") !== "false",
	};
};
