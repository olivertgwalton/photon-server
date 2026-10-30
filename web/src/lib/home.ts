import { client, need } from "#lib/api/client.ts";
import type { components } from "#lib/api/schema.d.ts";
import { keys } from "#lib/changes.ts";
import { type Row, rail, railLimit } from "#lib/rows.ts";
import { wallPageSize } from "#lib/wall.ts";

// A home row's first page, for the page that draws all of it.
export async function loadRow(
	fetch: typeof globalThis.fetch,
	depends: (...deps: `${string}:${string}`[]) => void,
	kind: components["schemas"]["HomeRowKind"],
) {
	depends(keys.home, keys.userdata);
	const page = await need(
		client(fetch).GET("/api/v1/profile/rows/{row}", {
			params: {
				path: { row: kind },
				query: { limit: wallPageSize, total: "exact" },
			},
		}),
	);
	return { kind, page };
}

type Card = components["schemas"]["Card"];
type Library = components["schemas"]["Library"];
type Section = components["schemas"]["HomeSection"];

// A row of the home, its titles on their way: as many as a rail shows.
type HomeRail = Row & {
	key: string;
	href: string;
	cards: Promise<{ items: Card[] }>;
};

// The home's rows: those the profile shows, in its order, a library's once for
// each library of the kinds it is made for, as Plex's library hubs are, in the
// profile's order of them.
export function homeRows(arrangement: Section[], libraries: Library[]) {
	return arrangement
		.filter((s) => s.visibility === "shown")
		.flatMap(({ row: kind, library_kinds: of }) => {
			const rows: Row[] = of
				? libraries
						.filter((l) => of.includes(l.kind))
						.map((l) => ({ kind, library: { id: l.id, name: l.name } }))
				: [{ kind }];
			return rows.map((row) => ({ ...row, ...rail(row) }));
		});
}

// The home's rows, each asked for on its own as Jellyfin's are.
export function homeRails(
	fetch: typeof globalThis.fetch,
	arrangement: Section[],
	libraries: Library[],
): HomeRail[] {
	const api = client(fetch);
	const limit = railLimit;
	return homeRows(arrangement, libraries).map((row) => ({
		...row,
		cards: need(
			row.library
				? api.GET("/api/v1/libraries/{id}/rows/{row}", {
						params: {
							path: { id: row.library.id, row: row.kind },
							query: { limit },
						},
					})
				: api.GET("/api/v1/profile/rows/{row}", {
						params: { path: { row: row.kind }, query: { limit } },
					}),
		),
	}));
}
