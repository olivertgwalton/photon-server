import type { Shape } from "./artwork.js";
import type { components } from "./api/schema.js";

type Kind = components["schemas"]["HomeRowKind"];

// A rail shows this many; the rest are its heading's to show, as the Photon
// apps' rows are twenty with the heading as the rest.
export const railLimit = 20;

export const homeRows: Record<Kind, { title: string; shape: Shape }> = {
	continue_watching: { title: "Continue Watching", shape: "still" },
	next_up: { title: "Next Up", shape: "still" },
	favourites: { title: "Favourites", shape: "poster" },
	recently_added_films: { title: "Recently Added Films", shape: "poster" },
	recently_added_shows: { title: "Recently Added Shows", shape: "poster" },
};

export function isHomeRow(kind: string): kind is Kind {
	return kind in homeRows;
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
