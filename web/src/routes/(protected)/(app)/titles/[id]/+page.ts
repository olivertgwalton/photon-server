import { error, redirect } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { episodesOf } from "#lib/seasons.js";
import type { components } from "#lib/api/schema.js";
import type { PageLoad } from "./$types";

type Title = components["schemas"]["TitlePage"];

export const load: PageLoad = async ({ fetch, params, parent, depends }) => {
	const api = client(fetch);
	const path = { params: { path: { id: params.id } } };
	const { title } = await parent();
	// A show or a season has no page of its own: it opens on the episode it is
	// at, as the apps open it, with its season beside it to pick another.
	if (title.kind === "show" || title.kind === "season") {
		redirect(307, `/titles/${await opening(api, title)}`);
	}
	const season = title.kind === "episode" ? title.season : undefined;
	if (season) depends(keys.title(season.id));
	const [members, themeMusic, seasonEpisodes, seasons] = await Promise.all([
		title.kind === "collection"
			? need(api.GET("/api/v1/titles/{id}/members", path))
			: undefined,
		// Asked only of a page with a tune to play.
		title.themes?.length
			? api
					.GET("/api/v1/profile/preferences")
					.then(({ data }) => data?.theme_music === "play")
			: false,
		// An episode is shown among the rest of its season, to pick another.
		season ? episodesOf(api, season.id) : undefined,
		// And its show's seasons, to pick another season's.
		title.kind === "episode" && title.show
			? api
					.GET("/api/v1/titles/{id}", {
						params: { path: { id: title.show.id } },
					})
					.then(({ data }) => data?.seasons ?? [])
			: undefined,
	]);
	return {
		members: members?.items,
		seasonEpisodes,
		seasons,
		themeMusic,
		// Not awaited: the page is drawn before the server has looked.
		similar:
			title.kind === "movie"
				? api
						.GET("/api/v1/titles/{id}/similar", path)
						.then(({ data }) => data?.items ?? [])
				: undefined,
	};
};

// The episode a show or season opens on: the one the profile is at, else the
// first of the season, or of the show's first numbered season.
async function opening(api: ReturnType<typeof client>, title: Title) {
	const { data: next } = await api.GET("/api/v1/titles/{id}/next", {
		params: { path: { id: title.id } },
	});
	if (next) return next.id;
	let first = title.episodes?.[0]?.id;
	if (title.kind === "show") {
		const season =
			title.seasons?.find((s) => s.number > 0) ?? title.seasons?.[0];
		if (season) first = (await episodesOf(api, season.id))?.[0]?.id;
	}
	if (!first) error(404, "It has no episodes.");
	return first;
}
