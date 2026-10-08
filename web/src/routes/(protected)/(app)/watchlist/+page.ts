import { loadRow } from "#lib/home.js";
import type { PageLoad } from "./$types";

// The watchlist is a page of its own, as Plex's is, not a row's.
export const load: PageLoad = ({ fetch, depends }) =>
	loadRow(fetch, depends, "watchlist");
