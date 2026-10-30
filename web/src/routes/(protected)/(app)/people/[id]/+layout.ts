import { client, need } from "#lib/api/client.ts";
import { keys } from "#lib/changes.ts";
import { workOf } from "#lib/credits.ts";
import type { LayoutLoad } from "./$types";

// A person, for their page and each craft's own page, loaded once.
export const load: LayoutLoad = async ({ fetch, params, depends }) => {
	depends(keys.userdata);
	const person = await need(
		client(fetch).GET("/api/v1/people/{id}", {
			params: { path: { id: params.id } },
		}),
	);
	return { person, work: workOf(person.credits) };
};
