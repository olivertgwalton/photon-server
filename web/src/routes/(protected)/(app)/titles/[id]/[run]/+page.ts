import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

const runs = {
	cast: "Cast & crew",
	extras: "Extras",
	collections: "Collections",
	similar: "More like this",
} as const;

// The whole of one of a title's rails.
export const load: PageLoad = async ({ fetch, params, depends }) => {
	const run = params.run;
	if (!(run in runs)) error(404, "There's no such row.");
	depends(keys.title(params.id), keys.userdata);
	const api = client(fetch);
	const path = { params: { path: { id: params.id } } };
	const [title, similar] = await Promise.all([
		need(api.GET("/api/v1/titles/{id}", path)),
		run === "similar"
			? need(api.GET("/api/v1/titles/{id}/similar", path))
			: undefined,
	]);
	return {
		run: run as keyof typeof runs,
		name: runs[run as keyof typeof runs],
		title,
		similar: similar?.items ?? [],
	};
};
