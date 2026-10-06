import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

const pageSize = 50;

export const load: PageLoad = async ({ fetch, url, depends }) => {
	depends(keys.userdata);
	const offset = Math.max(0, Number(url.searchParams.get("offset")) || 0);
	return {
		history: await need(
			client(fetch).GET("/api/v1/history", {
				params: { query: { offset, limit: pageSize } },
			}),
		),
		pageSize,
	};
};
