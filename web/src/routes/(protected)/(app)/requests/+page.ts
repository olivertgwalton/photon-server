import { client, need } from "#lib/api/client.ts";
import { offsetOf, pageSize } from "#lib/paging.ts";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, url }) => {
	const offset = offsetOf(url);
	return {
		requests: await need(
			client(fetch).GET("/api/v1/requests", {
				params: { query: { offset, limit: pageSize, total: "exact" } },
			}),
		),
	};
};
