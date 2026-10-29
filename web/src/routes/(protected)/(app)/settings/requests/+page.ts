import { client, need } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { keys } from "#lib/changes.js";
import { offsetOf, pageSize } from "#lib/paging.js";
import type { PageLoad } from "./$types";

type RequestState = components["schemas"]["RequestState"];

// Those waiting first: the page is where they are decided. ?state= is another
// state's, or every one's where it is "all". A request told as waiting, or
// decided elsewhere, loads them again.
export const load: PageLoad = async ({ fetch, url, depends }) => {
	depends(keys.requests);
	const asked = url.searchParams.get("state") ?? "pending";
	const state = asked === "all" ? undefined : (asked as RequestState);
	const offset = offsetOf(url);
	return {
		state: asked,
		requests: await need(
			client(fetch).GET("/api/v1/admin/requests", {
				params: { query: { state, offset, limit: pageSize, total: "exact" } },
			}),
		),
	};
};
