import type { Shape } from "./artwork.js";
import type { components } from "./api/schema.js";

type Kind = components["schemas"]["HomeRowKind"];

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
			cards: cards.filter((c) => c.kind === kind),
		}))
		.filter((g) => g.cards.length);
}
