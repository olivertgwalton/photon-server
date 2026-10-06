import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

const limit = 50;

export const load: PageLoad = async ({ fetch, url }) => {
	const api = client(fetch);
	const asked = url.searchParams.get("profile");
	const profile = asked && asked !== "all" ? asked : undefined;
	const offset = Math.max(Number(url.searchParams.get("offset")) || 0, 0);
	const [page, profiles] = await Promise.all([
		need(
			api.GET("/api/v1/admin/history", {
				params: { query: { profile, offset, limit } },
			}),
		),
		need(api.GET("/api/v1/profiles")),
	]);
	return { page, profile, limit, profiles: profiles.items };
};
