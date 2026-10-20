import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, url }) => {
	const q = url.searchParams.get("q")?.trim() ?? "";
	const library = url.searchParams.get("library") ?? undefined;
	if (!q) return { q, library, results: undefined };
	return {
		q,
		library,
		results: await need(
			client(fetch).GET("/api/v1/search", {
				params: { query: { q, library, limit: 100 } },
			}),
		),
	};
};
