import type { Shape } from "./artwork.js";
import type { components } from "./api/schema.js";
import { type WallQuery, wallSearch } from "./wall.js";

type Kind = components["schemas"]["HomeRowKind"];
type Row = components["schemas"]["HomeRow"];

// A rail shows this many; the rest are its heading's to show, as the Photon
// apps' rows are twenty with the heading as the rest.
export const railLimit = 20;

// How each kind of row is drawn. A row of a library's titles is one for each
// library, as Plex's library hubs are, leading to that library's wall in the
// row's order. The server names every row.
export const homeRows: Record<
	Kind,
	{
		shape: Shape;
		library?: { wall: WallQuery };
		// What its own page says where it has nothing.
		empty?: string;
	}
> = {
	continue_watching: { shape: "still" },
	next_up: { shape: "still" },
	watchlist: {
		shape: "poster",
		empty:
			"Nothing here yet. Choose the bookmark on a film or show to keep it here until it is watched.",
	},
	favourites: { shape: "poster" },
	recently_added_films: {
		shape: "poster",
		library: { wall: { sort: "added" } },
	},
	recently_added_shows: {
		shape: "poster",
		library: { wall: { sort: "added" } },
	},
	recently_released: {
		shape: "poster",
		library: { wall: { sort: "released" } },
	},
	top_rated_unwatched: {
		shape: "poster",
		library: {
			wall: { sort: "rating", mark: ["unwatched"] },
		},
	},
	// A row each, under the collection's own name, leading to its page.
	collection: { shape: "poster" },
};

// The rows with a page of their own, as Plex's Watchlist and Jellyfin's
// Favorites are, rather than the row's.
export const rowPages = {
	watchlist: "/watchlist",
	favourites: "/favourites",
} as const satisfies Partial<Record<Kind, string>>;

export function isHomeRow(kind: string): kind is Kind {
	return kind in homeRows;
}

// What a home row is drawn as: its key among the rows, and where its heading
// leads.
export function rail(row: Row): { key: string; href: string } {
	const kind = homeRows[row.kind];
	if (row.collection) {
		return {
			key: row.collection.id,
			href: `/titles/${row.collection.id}`,
		};
	}
	if (row.library && kind.library) {
		return {
			key: `${row.kind}/${row.library.id}`,
			href: `/libraries/${row.library.id}${wallSearch(kind.library.wall)}`,
		};
	}
	return {
		key: row.kind,
		href:
			row.kind in rowPages
				? rowPages[row.kind as keyof typeof rowPages]
				: `/home/${row.kind}`,
	};
}

const kinds: [components["schemas"]["ItemKind"], string][] = [
	["movie", "Films"],
	["show", "Shows"],
	["season", "Seasons"],
	["episode", "Episodes"],
	["collection", "Collections"],
	["extra", "Extras"],
];

// A mixed list of titles as sections, one per kind, in the order above.
export function byKind(cards: components["schemas"]["Card"][]) {
	return kinds
		.map(([kind, name]) => ({
			kind,
			name,
			slug: name.toLowerCase(),
			cards: cards.filter((c) => c.kind === kind),
		}))
		.filter((g) => g.cards.length);
}

// A list with the item at from moved to to, the rest keeping their order.
export function moved<T>(list: T[], from: number, to: number): T[] {
	const out = [...list];
	const [item] = out.splice(from, 1);
	out.splice(Math.max(0, Math.min(to, out.length)), 0, item);
	return out;
}
