import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.admin.playbacks, keys.admin.libraries, keys.admin.jobs);
	const api = client(fetch);
	const [playbacks, activity, libraries, jobs, providers] = await Promise.all([
		need(api.GET("/api/v1/admin/playbacks")),
		need(
			api.GET("/api/v1/admin/activity", { params: { query: { limit: 10 } } }),
		),
		need(api.GET("/api/v1/admin/libraries")),
		need(api.GET("/api/v1/admin/jobs")),
		need(api.GET("/api/v1/admin/providers")),
	]);
	return {
		playbacks,
		activity: activity.items,
		libraries: libraries.items,
		dead: jobs.dead.length,
		providers: providers.items,
	};
};
