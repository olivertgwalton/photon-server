import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { byKind } from "#lib/rows.js";
import type { PageLoad } from "./$types";

// The most the server answers at once. It pages a search's titles of every
// kind together, so one kind's whole is what the first page of them holds.
const most = 200;

// The whole of one run of a search's results: a kind of title, or people.
export const load: PageLoad = async ({ fetch, params, url, parent }) => {
	const q = url.searchParams.get("q")?.trim() ?? "";
	const library = url.searchParams.get("library") ?? undefined;
	const results = await need(
		client(fetch).GET("/api/v1/search", {
			params: { query: { q, library, limit: most } },
		}),
	);
	if (params.run === "people") {
		return { q, name: "People", people: results.people, cards: undefined };
	}
	const { words } = await parent();
	const group = byKind(results.items, words.kinds).find(
		(g) => g.kind === params.run,
	);
	if (!group) error(404, "Nothing of that kind matches.");
	return {
		q,
		name: group.name,
		people: undefined,
		cards: group.cards,
		kind: group.kind,
	};
};
