import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => ({
	trackers: (await need(client(fetch).GET("/api/v1/admin/trackers"))).items,
});
