import { client, need } from "#lib/api/client.js";
import { returnPath } from "#lib/session.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, parent, url }) => {
	const [{ items }, { me }] = await Promise.all([
		need(client(fetch).GET("/api/v1/profiles")),
		parent(),
	]);
	return {
		profiles: items,
		current: me.id,
		// A locked profile, once chosen, asks for its PIN or password.
		chosen: items.find((p) => p.id === url.searchParams.get("profile")),
		to: returnPath(url),
	};
};
