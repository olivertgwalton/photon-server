import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, url }) => {
	const q = url.searchParams.get("q")?.trim() ?? "";
	const library = url.searchParams.get("library") ?? undefined;
	if (!q) return { q, library, results: undefined, server: "" };
	const api = client(fetch);
	const [results, server] = await Promise.all([
		need(
			api.GET("/api/v1/search", {
				params: { query: { q, library, limit: 100 } },
			}),
		),
		// Its name heads what it does not hold yet.
		need(api.GET("/api/v1/server")),
	]);
	return { q, library, results, server: server.name };
};
