import type { Shape } from "./artwork.js";
import type { components } from "./api/schema.js";

type Kind = components["schemas"]["HomeRowKind"];
type LibraryKind = components["schemas"]["LibraryKind"];
// A row as the home draws it: of a kind, and a library's of that library.
export type Row = { kind: Kind; library?: { id: string; name: string } };

// A rail shows this many, as the Photon apps' rows are twenty.
export const railLimit = 20;

// How each kind of row is drawn. A row of a library's titles is one for each
// library of the kinds it is `of`, as Plex's library hubs are, leading to a
// page of what its rail holds.
export const homeRows: Record<
	Kind,
	{
		shape: Shape;
		of?: LibraryKind[];
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
	recently_added_films: { shape: "poster", of: ["movies"] },
	recently_added_shows: { shape: "poster", of: ["shows"] },
	recently_released: { shape: "poster", of: ["movies", "shows"] },
	top_rated_unwatched: { shape: "poster", of: ["movies", "shows"] },
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
	if (row.library && kind.of) {
		return {
			key: `${row.kind}/${row.library.id}`,
			href: `/libraries/${row.library.id}/rows/${row.kind}`,
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

const kinds: components["schemas"]["ItemKind"][] = [
	"movie",
	"show",
	"season",
	"episode",
	"collection",
	"extra",
];

// A mixed list of titles as sections, one per kind, in the order above, each
// headed by its kind's name.
export function byKind(
	cards: components["schemas"]["Card"][],
	names: components["schemas"]["Vocabulary"]["kinds"],
) {
	return kinds
		.map((kind) => ({
			kind,
			name: names[kind],
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
