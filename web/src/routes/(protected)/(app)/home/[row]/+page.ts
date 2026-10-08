import { error } from "@sveltejs/kit";
import { loadRow } from "#lib/home.js";
import { homeRows, isHomeRow, rowPages } from "#lib/rows.js";
import type { PageLoad } from "./$types";

// One home row, a page at a time.
export const load: PageLoad = ({ fetch, params, depends }) => {
	const kind = params.row;
	// A collection's and a library's rows, and those with a page of their own,
	// lead to that page instead.
	if (
		!isHomeRow(kind) ||
		kind === "collection" ||
		homeRows[kind].library ||
		kind in rowPages
	)
		error(404, "There's no such row.");
	return loadRow(fetch, depends, kind);
};
