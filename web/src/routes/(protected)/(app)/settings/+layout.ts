import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { LayoutLoad } from "./$types";

// How many requests wait on the admin or manager reading, drawn beside
// Requests, and counted again as the socket tells of one.
export const load: LayoutLoad = async ({ fetch, parent, depends }) => {
	depends(keys.requests);
	const { me } = await parent();
	if (me.role === "user") return { waiting: 0 };
	const waiting = await need(
		client(fetch).GET("/api/v1/admin/requests", {
			params: { query: { state: "pending", limit: 1, total: "exact" } },
		}),
	);
	return { waiting: waiting.total ?? 0 };
};
