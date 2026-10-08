import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.admin.backups);
	return { backups: await need(client(fetch).GET("/api/v1/admin/backups")) };
};
