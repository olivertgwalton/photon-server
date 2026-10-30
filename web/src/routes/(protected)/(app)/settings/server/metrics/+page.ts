import { client, need } from "#lib/api/client.ts";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	return { metrics: await need(client(fetch).GET("/api/v1/admin/metrics")) };
};
