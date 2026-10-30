import { error } from "@sveltejs/kit";
import { loadRow } from "#lib/home.ts";
import { isHomeRow, rowPages } from "#lib/rows.ts";
import type { PageLoad } from "./$types";

// One home row, a page at a time.
export const load: PageLoad = ({ fetch, params, depends }) => {
	const kind = params.row;
	// Those with a page of their own lead to that page instead, as a library's
	// rows lead to the library's, which the server refuses here.
	if (!isHomeRow(kind) || kind in rowPages) error(404, "There's no such row.");
	return loadRow(fetch, depends, kind);
};
