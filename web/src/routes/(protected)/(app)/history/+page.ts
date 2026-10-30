import { client, need } from "#lib/api/client.ts";
import { keys } from "#lib/changes.ts";
import { afterOf, pageSize } from "#lib/paging.ts";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, url, depends }) => {
	depends(keys.userdata);
	const after = afterOf(url);
	return {
		history: await need(
			client(fetch).GET("/api/v1/history", {
				params: { query: { after, limit: pageSize } },
			}),
		),
	};
};
