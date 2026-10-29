import type { components } from "./api/schema.js";
import type { Row } from "./rows.js";

type Card = components["schemas"]["Card"];

// A home row as the showcase reads it: its key, whether it is a library's,
// and its titles on their way.
type Source = Row & { key: string; cards: Promise<{ items: Card[] }> };

// The showcase is drawn or not, and shows the curated pick or a row the reader
// chose, by its key. Kept in this browser, as the apps keep it on each device.
export type Showcase = {
	visibility: "shown" | "hidden";
	row?: string;
};

const key = "showcase";

export function readShowcase(): Showcase {
	try {
		const kept = JSON.parse(localStorage.getItem(key) ?? "null");
		if (kept?.visibility === "shown" || kept?.visibility === "hidden")
			return kept;
	} catch {}
	return { visibility: "shown" };
}

export function keepShowcase(s: Showcase) {
	localStorage.setItem(key, JSON.stringify(s));
}

// The titles the showcase is about: the row the reader chose, which slides
// through itself; or else the first library row that has any. Never a row
// about the reader on its own account: the showcase is a suggestion, and what
// they already chose suggests nothing.
export async function showcasePool(
	rails: Source[],
	row: string | undefined,
): Promise<{ cards: Card[]; turn: "slides" | "rests" }> {
	const chosen = rails.find((r) => r.key === row);
	if (chosen) {
		const cards = await items(chosen);
		if (cards.length) return { cards, turn: "slides" };
	}
	for (const r of rails) {
		if (!r.library) continue;
		const cards = await items(r);
		if (cards.length) return { cards, turn: "rests" };
	}
	return { cards: [], turn: "rests" };
}

async function items(r: Source): Promise<Card[]> {
	return r.cards.then(
		(p) => p.items,
		() => [],
	);
}
