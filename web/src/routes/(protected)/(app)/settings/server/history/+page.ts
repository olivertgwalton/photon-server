import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { offsetOf, pageSize } from "#lib/paging.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, url, depends }) => {
	depends(keys.admin.profiles);
	const api = client(fetch);
	const asked = url.searchParams.get("profile");
	const profile = asked && asked !== "all" ? asked : undefined;
	const offset = offsetOf(url);
	const [page, profiles] = await Promise.all([
		need(
			api.GET("/api/v1/admin/history", {
				params: { query: { profile, offset, limit: pageSize } },
			}),
		),
		need(api.GET("/api/v1/profiles")),
	]);
	return { page, profile, profiles: profiles.items };
};
