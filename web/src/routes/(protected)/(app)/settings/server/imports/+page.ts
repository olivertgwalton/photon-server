import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [imports, profiles] = await Promise.all([
		need(api.GET("/api/v1/admin/imports")),
		need(api.GET("/api/v1/profiles")),
	]);
	return { imports: imports.items, profiles: profiles.items };
};
