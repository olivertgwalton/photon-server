import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import { castOf } from "#lib/credits.js";
import { extrasOf } from "#lib/extras.js";
import type { LayoutLoad } from "./$types";

// A title, for its page and each of its rails' own pages, loaded once. One the
// server does not hold is, where a search found it for the profile to ask for,
// drawn as its page would be from what its provider says of it.
export const load: LayoutLoad = async ({ fetch, params, depends }) => {
	depends(keys.title(params.id), keys.userdata);
	const title = await need(
		client(fetch).GET("/api/v1/titles/{id}", {
			params: { path: { id: params.id } },
		}),
	);
	return {
		title,
		asking: title.requestable === true,
		// One card per person, whatever they did on it.
		cast: castOf(title.credits ?? []),
		extras: extrasOf(title),
		collections: (title.collections ?? []).map((c) => ({
			...c,
			kind: "collection" as const,
		})),
	};
};
