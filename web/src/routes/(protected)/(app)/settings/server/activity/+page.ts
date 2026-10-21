import { client, need } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { afterOf, pageSize } from "#lib/paging.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, url, parent }) => {
	const api = client(fetch);
	const asked = url.searchParams.get("kind") ?? "";
	const { words } = await parent();
	const kind =
		asked in words.logged
			? (asked as components["schemas"]["EventKind"])
			: undefined;
	const after = afterOf(url);
	const page = await need(
		api.GET("/api/v1/admin/activity", {
			params: { query: { kind, after, limit: pageSize } },
		}),
	);
	return { page, kind, first: after === undefined };
};
