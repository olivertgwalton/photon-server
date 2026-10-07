import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { homeRows, isHomeRow } from "#lib/rows.js";
import { wallPageSize } from "#lib/wall.js";
import type { PageLoad } from "./$types";

// One home row, a page at a time.
export const load: PageLoad = async ({ fetch, params, depends }) => {
	const kind = params.row;
	// A collection's and a library's rows lead to its own page instead.
	if (!isHomeRow(kind) || kind === "collection" || homeRows[kind].library)
		error(404, "There's no such row.");
	depends(keys.home, keys.userdata);
	const page = await need(
		client(fetch).GET("/api/v1/home/{row}", {
			params: { path: { row: kind }, query: { limit: wallPageSize } },
		}),
	);
	return { kind, page };
};
