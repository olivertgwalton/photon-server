import { error, redirect } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { keys } from "#lib/changes.js";
import { episodesOf } from "#lib/seasons.js";
import type { PageLoad } from "./$types";

type Title = components["schemas"]["TitlePage"];

export const load: PageLoad = async ({ fetch, params, parent, depends }) => {
	const api = client(fetch);
	const path = { params: { path: { id: params.id } } };
	const { title, asking } = await parent();
	// A season has no page of its own: it opens on the episode it is at, with
	// its season beside it to pick another.
	if (title.kind === "season") {
		redirect(307, `/titles/${await opening(api, title)}`);
	}
	const season = title.kind === "episode" ? title.season : undefined;
	if (season) depends(keys.title(season.id));
	const [members, next, seasonEpisodes, show, showSimilar] = await Promise.all([
		title.kind === "collection"
			? need(api.GET("/api/v1/titles/{id}/members", path))
			: undefined,
		// A show plays the episode the profile is at; finished, none.
		title.kind === "show" && !asking
			? api.GET("/api/v1/titles/{id}/next", path).then(({ data }) => data)
			: undefined,
		// An episode is shown among the rest of its season, to pick another.
		season ? episodesOf(api, season.id) : undefined,
		// And its show, whose seasons it picks another season's from, and whose
		// extras, box sets, links and likes it shows.
		title.kind === "episode" && title.show
			? api
					.GET("/api/v1/titles/{id}", {
						params: { path: { id: title.show.id } },
					})
					.then(({ data }) => data)
			: undefined,
		// Awaited, unlike a film's: picking another episode keeps the page
		// where it was, and a row drawn after the page would move it.
		title.kind === "episode" && title.show
			? similarOf(api, title.show.id)
			: undefined,
	]);
	return {
		members: members?.items,
		next,
		seasonEpisodes,
		show,
		seasons: show?.seasons,
		// Asked only of a page with a tune to play, and not waited for: the page
		// is drawn at once and the tune starts once the answer comes.
		themeMusic: title.themes?.length
			? api
					.GET("/api/v1/profile/preferences")
					.then(({ data }) => data?.theme_music === "play")
					.catch(() => false)
			: false,
		// Not awaited: the page is drawn before the server has looked.
		similar:
			(title.kind === "movie" || title.kind === "show") && !asking
				? similarOf(api, title.id)
				: showSimilar,
	};
};

// The episode a season opens on: the one the profile is at, else its first.
async function opening(api: ReturnType<typeof client>, title: Title) {
	const { data: next } = await api.GET("/api/v1/titles/{id}/next", {
		params: { path: { id: title.id } },
	});
	const first = next?.id ?? title.episodes?.[0]?.id;
	if (!first) error(404, "It has no episodes.");
	return first;
}

function similarOf(api: ReturnType<typeof client>, id: string) {
	return api
		.GET("/api/v1/titles/{id}/similar", { params: { path: { id } } })
		.then(({ data }) => data?.items ?? []);
}
