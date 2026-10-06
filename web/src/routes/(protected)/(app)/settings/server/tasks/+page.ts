import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends("admin:tasks");
	const { items } = await need(client(fetch).GET("/api/v1/admin/tasks"));
	return { tasks: items };
};
