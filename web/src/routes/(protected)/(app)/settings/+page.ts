import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, parent }) => {
	const api = client(fetch);
	const [profiles, authenticator, server, { me }] = await Promise.all([
		need(api.GET("/api/v1/profiles")),
		// None is a 404.
		api.GET("/api/v1/profile/authenticator"),
		need(api.GET("/api/v1/server")),
		parent(),
	]);
	const mine = profiles.items.find((p) => p.id === me.id);
	return {
		lock: mine?.lock ?? "password",
		authenticator: authenticator.data,
		server: server.name,
	};
};
