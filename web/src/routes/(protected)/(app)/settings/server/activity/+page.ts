import type { components } from "#lib/api/schema.js";
import { loggedKinds } from "#lib/admin/words.js";
import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

const limit = 50;

export const load: PageLoad = async ({ fetch, url }) => {
	const api = client(fetch);
	const asked = url.searchParams.get("kind") ?? "";
	const kind =
		asked in loggedKinds
			? (asked as components["schemas"]["EventKind"])
			: undefined;
	const offset = Math.max(Number(url.searchParams.get("offset")) || 0, 0);
	const page = await need(
		api.GET("/api/v1/admin/activity", {
			params: { query: { kind, offset, limit } },
		}),
	);
	return { page, kind, limit };
};
