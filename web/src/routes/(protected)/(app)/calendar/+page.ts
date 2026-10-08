import { client, need } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { monthOf, today, upcoming, weeks } from "#lib/calendar.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

type Filter = components["schemas"]["CalendarFilter"];

const filters: Filter[] = ["mine", "watchlist", "favourites", "all"];

export const load: PageLoad = async ({ fetch, url, depends }) => {
	depends(keys.home, keys.userdata);
	const now = new Date();
	// The upcoming list unless the month is asked for; the profile's own shows
	// unless another filter is, as Silo's calendar follows what it follows.
	const view = url.searchParams.get("view") === "month" ? "month" : "upcoming";
	const asked = url.searchParams.get("filter") as Filter;
	const filter = filters.includes(asked) ? asked : "mine";
	const month = monthOf(url.searchParams.get("month"), now);
	const days = view === "month" ? weeks(month) : upcoming(today(now));
	return {
		view,
		filter,
		filters,
		month,
		days,
		calendar: await need(
			client(fetch).GET("/api/v1/calendar", {
				params: {
					query: { start: days[0], end: days[days.length - 1], filter },
				},
			}),
		),
	};
};
