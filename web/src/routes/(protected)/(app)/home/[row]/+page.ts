import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { isHomeRow } from "#lib/rows.js";
import type { PageLoad } from "./$types";

// The whole of one home row, as many as the server lists in one.
export const load: PageLoad = async ({ fetch, params, depends }) => {
	const kind = params.row;
	if (!isHomeRow(kind) || kind === "collection")
		error(404, "There's no such row.");
	depends(keys.home, keys.userdata);
	const home = await need(
		client(fetch).GET("/api/v1/home", { params: { query: { limit: 200 } } }),
	);
	return { kind, cards: home.rows.find((r) => r.kind === kind)?.items ?? [] };
};
