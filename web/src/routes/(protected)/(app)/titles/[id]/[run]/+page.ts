import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

const runs = {
	cast: "Cast & crew",
	extras: "Extras",
	collections: "Collections",
	similar: "More like this",
} as const;

// The whole of one of a title's rails.
export const load: PageLoad = async ({ fetch, params }) => {
	const run = params.run;
	if (!(run in runs)) error(404, "There's no such row.");
	const similar =
		run === "similar"
			? await need(
					client(fetch).GET("/api/v1/titles/{id}/similar", {
						params: { path: { id: params.id } },
					}),
				)
			: undefined;
	return {
		run: run as keyof typeof runs,
		name: runs[run as keyof typeof runs],
		similar: similar?.items ?? [],
	};
};
