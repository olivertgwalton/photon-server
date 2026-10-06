import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params, depends }) => {
	depends(keys.title(params.id), keys.userdata);
	const api = client(fetch);
	const path = { params: { path: { id: params.id } } };
	const title = await need(api.GET("/api/v1/titles/{id}", path));
	const [members, next, themeMusic] = await Promise.all([
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
					.GET("/api/v1/me/preferences")
					.then(({ data }) => data?.theme_music === "play")
			: false,
	]);
	return {
		title,
		members: members?.items,
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
