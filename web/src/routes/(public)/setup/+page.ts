import { redirect } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { LOGIN } from "#lib/session.js";
import type { PageLoad } from "./$types";

// The server says whether it is still to be set up, and by whom; a server set
// up already is logged in to.
export const load: PageLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [setup, server] = await Promise.all([
		need(api.GET("/api/v1/setup")),
		need(api.GET("/api/v1/server")),
	]);
	if (setup.state === "done") redirect(303, LOGIN);
	return { state: setup.state, server };
};
