import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, parent }) => {
	const [profiles, { me }] = await Promise.all([
		need(client(fetch).GET("/api/v1/profiles")),
		parent(),
	]);
	const mine = profiles.items.find((p) => p.id === me.id);
	return { lock: mine?.lock ?? "password" };
};
