import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends("admin:playbacks", "admin:libraries");
	const api = client(fetch);
	const [playbacks, activity, libraries, profiles] = await Promise.all([
		need(api.GET("/api/v1/admin/playbacks")),
		need(
			api.GET("/api/v1/admin/activity", { params: { query: { limit: 10 } } }),
		),
		need(api.GET("/api/v1/admin/libraries")),
		need(api.GET("/api/v1/profiles")),
	]);
	return {
		playbacks,
		activity: activity.items,
		libraries: libraries.items,
		profiles: profiles.items,
	};
};
