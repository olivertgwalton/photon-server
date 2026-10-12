import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { offsetOf, pageSize } from "#lib/paging.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, url, depends }) => {
	depends(keys.userdata);
	const offset = offsetOf(url);
	return {
		history: await need(
			client(fetch).GET("/api/v1/history", {
				params: { query: { offset, limit: pageSize } },
			}),
		),
	};
};
