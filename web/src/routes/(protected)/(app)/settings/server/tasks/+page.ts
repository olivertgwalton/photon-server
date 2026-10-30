import { client, need } from "#lib/api/client.ts";
import { keys } from "#lib/changes.ts";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.admin.tasks);
	const api = client(fetch);
	const [tasks, maintenance] = await Promise.all([
		need(api.GET("/api/v1/admin/tasks")),
		need(api.GET("/api/v1/admin/maintenance")),
	]);
	return { tasks: tasks.items, maintenance };
};
