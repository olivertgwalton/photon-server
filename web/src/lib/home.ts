import { client, need } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { keys } from "#lib/changes.js";
import { homeRows, type Row, rail, railLimit } from "#lib/rows.js";
import { wallPageSize } from "#lib/wall.js";

// A home row's first page, for the page that draws all of it.
export async function loadRow(
	fetch: typeof globalThis.fetch,
	depends: (...deps: `${string}:${string}`[]) => void,
	kind: components["schemas"]["HomeRowKind"],
) {
	depends(keys.home, keys.userdata);
	const page = await need(
		client(fetch).GET("/api/v1/profile/rows/{row}", {
			params: { path: { row: kind }, query: { limit: wallPageSize } },
		}),
	);
	return { kind, page };
}

type Card = components["schemas"]["Card"];
type Library = components["schemas"]["Library"];
type Section = components["schemas"]["HomeSection"];

// A row of the home, its titles on their way: one more than a rail shows, so
// it knows whether there is more to see.
export type HomeRail = Row & {
	key: string;
	href: string;
	cards: Promise<{ items: Card[]; total?: number }>;
};

// The home's rows, each asked for on its own as Jellyfin's are: those the
// profile shows, in its order, a library's once for each library it is made
// for, in the profile's order of them.
export function homeRails(
	fetch: typeof globalThis.fetch,
	arrangement: Section[],
	libraries: Library[],
): HomeRail[] {
	const api = client(fetch);
	const limit = railLimit + 1;
	return arrangement
		.filter((s) => s.visibility === "shown")
		.flatMap(({ row: kind }): HomeRail[] => {
			const of = homeRows[kind].of;
			if (!of) {
				const row = { kind };
				return [
					{
						...row,
						...rail(row),
						cards: need(
							api.GET("/api/v1/profile/rows/{row}", {
								params: { path: { row: kind }, query: { limit } },
							}),
						),
					},
				];
			}
			return libraries
				.filter((l) => of.includes(l.kind))
				.map((l) => {
					const row = { kind, library: { id: l.id, name: l.name } };
					return {
						...row,
						...rail(row),
						cards: need(
							api.GET("/api/v1/libraries/{id}/rows/{row}", {
								params: { path: { id: l.id, row: kind }, query: { limit } },
							}),
						),
					};
				});
		});
}
