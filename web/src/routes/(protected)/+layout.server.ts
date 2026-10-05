import { redirect } from "@sveltejs/kit";
import { loginPath } from "#lib/server/session.js";
import type { LayoutServerLoad } from "./$types";

// Every page under (protected) needs a session; this is the one place that
// says so. A page's own load may run beside this one, so a call it makes
// without a session is refused by the API and sent here by `need`.
export const load: LayoutServerLoad = ({ locals, url }) => {
	if (!locals.session) redirect(303, loginPath(url));
	return { profile: locals.session.profile };
};
