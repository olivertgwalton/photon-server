import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { restoring } from "#lib/restoring.svelte.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.admin.backups);
	const backups = await need(client(fetch).GET("/api/v1/admin/backups"));
	// A restore under way, as the server says here, is shown as one told live is.
	if (backups.restoring) restoring.on = true;
	return { backups };
};
