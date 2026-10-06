import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params }) => {
	const api = client(fetch);
	const path = { params: { path: { id: params.id } } };
	const [profiles, access, libraries] = await Promise.all([
		need(api.GET("/api/v1/profiles")),
		need(api.GET("/api/v1/admin/profiles/{id}/access", path)),
		need(api.GET("/api/v1/admin/libraries")),
	]);
	const profile = profiles.items.find((p) => p.id === params.id);
	if (!profile) error(404, "That isn't here any more.");
	return { profile, access, libraries: libraries.items };
};
