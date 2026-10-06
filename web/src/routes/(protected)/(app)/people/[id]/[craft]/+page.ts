import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { workOf } from "#lib/credits.js";
import type { PageLoad } from "./$types";

// The whole of one craft of a person's work.
export const load: PageLoad = async ({ fetch, params, depends }) => {
	depends(keys.userdata);
	const person = await need(
		client(fetch).GET("/api/v1/people/{id}", {
			params: { path: { id: params.id } },
		}),
	);
	const group = workOf(person.credits).find((g) => g.slug === params.craft);
	if (!group) error(404, `${person.name} has no such work here.`);
	return { person, group };
};
