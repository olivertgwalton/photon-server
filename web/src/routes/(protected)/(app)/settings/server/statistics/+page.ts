import { client, need } from "#lib/api/client.ts";
import { keys } from "#lib/changes.ts";
import type { PageLoad } from "./$types";

// The spans a report is read over, in days.
const spans = [7, 30, 90, 365];

export const load: PageLoad = async ({ fetch, url, depends }) => {
	depends(keys.admin.profiles);
	const api = client(fetch);
	const asked = url.searchParams.get("profile");
	const profile = asked && asked !== "all" ? asked : undefined;
	const wanted = Number(url.searchParams.get("days"));
	const days = spans.includes(wanted) ? wanted : 30;
	// Days are counted where the admin reading it is, so last night's film is
	// last night's.
	const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
	const [stats, profiles] = await Promise.all([
		need(
			api.GET("/api/v1/admin/statistics", {
				params: { query: { days, zone, profile } },
			}),
		),
		need(api.GET("/api/v1/profiles")),
	]);
	return { stats, profile, days, profiles: profiles.items };
};
