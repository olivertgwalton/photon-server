import { redirect } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

// The dashboard's old addresses, now Settings' server pages.
export const load: PageLoad = ({ params, url }) => {
	const rest = params.rest ? `/${params.rest}` : "";
	redirect(308, `/settings/server${rest}${url.search}`);
};
