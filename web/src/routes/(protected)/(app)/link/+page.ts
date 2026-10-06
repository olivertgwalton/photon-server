import { redirect } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

// The address a television shows (the server's verification_uri), kept as
// Settings' own page took its place.
export const load: PageLoad = ({ url }) => {
	redirect(308, `/settings/link${url.search}`);
};
