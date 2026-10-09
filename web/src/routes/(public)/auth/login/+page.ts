import { redirect } from "@sveltejs/kit";
import { client } from "#lib/api/client.js";
import { returnPath, SETUP } from "#lib/session.js";
import type { PageLoad } from "./$types";

// A reader already signed in goes on, and one reaching a server still to be
// set up goes to set it up; a server that isn't answering is said so on the
// page.
export const load: PageLoad = async ({ fetch, url }) => {
	const api = client(fetch);
	const [me, server, setup, providers] = await Promise.all([
		api.GET("/api/v1/profile").catch(() => null),
		api.GET("/api/v1/server").catch(() => null),
		api.GET("/api/v1/setup").catch(() => null),
		api.GET("/api/v1/auth/sign-in-providers").catch(() => null),
	]);
	if (me?.data) redirect(303, returnPath(url));
	if (setup?.data && setup.data.state !== "done") redirect(303, SETUP);
	return {
		server: server?.data ?? null,
		providers: providers?.data?.items ?? [],
	};
};
