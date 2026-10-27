import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { offsetOf, pageSize } from "#lib/paging.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, url, depends }) => {
	depends(keys.requests);
	const offset = offsetOf(url);
	return {
		requests: await need(
			client(fetch).GET("/api/v1/requests", {
				params: { query: { offset, limit: pageSize, total: "exact" } },
			}),
		),
	};
};
