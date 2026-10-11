import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { homeRows, isHomeRow } from "#lib/rows.js";
import type { PageLoad } from "./$types";

// One of a library's rows whole, as the Photon apps open it from its heading.
export const load: PageLoad = async ({ fetch, params, parent, depends }) => {
	const kind = params.row;
	if (!isHomeRow(kind) || !homeRows[kind].whole)
		error(404, "There's no such row.");
	depends(keys.home, keys.userdata, keys.library(params.id));
	const [{ libraries }, row] = await Promise.all([
		parent(),
		need(
			client(fetch).GET("/api/v1/libraries/{id}/rows/{row}", {
				params: { path: { id: params.id, row: kind } },
			}),
		),
	]);
	const library = libraries.find((l) => l.id === params.id);
	if (!library) error(404, "That library isn't here any more.");
	return { kind, library, items: row.items };
};
