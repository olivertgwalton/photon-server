import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.admin.profiles);
	const api = client(fetch);
	const [imports, profiles] = await Promise.all([
		need(api.GET("/api/v1/admin/imports")),
		need(api.GET("/api/v1/profiles")),
	]);
	return { imports: imports.items, profiles: profiles.items };
};
