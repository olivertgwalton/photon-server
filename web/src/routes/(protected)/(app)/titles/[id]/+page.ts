import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params, parent, depends }) => {
	const api = client(fetch);
	const path = { params: { path: { id: params.id } } };
	const { title } = await parent();
	const season = title.kind === "episode" ? title.season : undefined;
	if (season) depends(keys.title(season.id));
	const [members, next, themeMusic, seasonEpisodes] = await Promise.all([
		title.kind === "collection"
			? need(api.GET("/api/v1/titles/{id}/members", path))
			: undefined,
		// A show or season plays the episode the profile is at; finished, none.
		title.kind === "show" || title.kind === "season"
			? api.GET("/api/v1/titles/{id}/next", path).then(({ data }) => data)
			: undefined,
		// Asked only of a page with a tune to play.
		title.themes?.length
			? api
					.GET("/api/v1/profile/preferences")
					.then(({ data }) => data?.theme_music === "play")
			: false,
		// An episode is shown among the rest of its season, to pick another.
		season
			? api
					.GET("/api/v1/titles/{id}", {
						params: { path: { id: season.id } },
					})
					.then(({ data }) =>
						data?.episodes?.map((e) => ({ ...e, kind: "episode" as const })),
					)
			: undefined,
	]);
	return {
		members: members?.items,
		seasonEpisodes,
		next,
		themeMusic,
		// Not awaited: the page is drawn before the server has looked.
		similar:
			title.kind === "movie" || title.kind === "show"
				? api
						.GET("/api/v1/titles/{id}/similar", path)
						.then(({ data }) => data?.items ?? [])
				: undefined,
	};
};
