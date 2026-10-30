import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.ts";
import { keys } from "#lib/changes.ts";
import { isHomeRow, railLimit } from "#lib/rows.ts";
import type { PageLoad } from "./$types";

// What one of a library's rails holds, as the Photon apps open it from its
// heading.
export const load: PageLoad = async ({ fetch, params, parent, depends }) => {
	const kind = params.row;
	// A row not made for the library's kind the server refuses.
	if (!isHomeRow(kind)) error(404, "There's no such row.");
	depends(keys.home, keys.userdata, keys.library(params.id));
	const [{ libraries }, row] = await Promise.all([
		parent(),
		need(
			client(fetch).GET("/api/v1/libraries/{id}/rows/{row}", {
				params: {
					path: { id: params.id, row: kind },
					query: { limit: railLimit },
				},
			}),
		),
	]);
	const library = libraries.find((l) => l.id === params.id);
	if (!library) error(404, "That library isn't here any more.");
	return { kind, library, items: row.items };
};
