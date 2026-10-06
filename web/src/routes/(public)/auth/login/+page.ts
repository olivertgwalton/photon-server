import { redirect } from "@sveltejs/kit";
import { client } from "#lib/api/client.js";
import { returnPath } from "#lib/session.js";
import type { PageLoad } from "./$types";

// A reader already signed in goes on; a server that isn't answering is said
// so on the page.
export const load: PageLoad = async ({ fetch, url }) => {
	const api = client(fetch);
	const [me, server] = await Promise.all([
		api.GET("/api/v1/me").catch(() => null),
		api.GET("/api/v1/server").catch(() => null),
	]);
	if (me?.data) redirect(303, returnPath(url));
	return { server: server?.data ?? null };
};
