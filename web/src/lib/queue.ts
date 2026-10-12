import type { components } from "#lib/api/schema.js";
import { goto } from "$app/navigation";

type Card = components["schemas"]["Card"];

// What the player goes on to, in turn: a playlist, as it is or shuffled. It is
// the tab's own and gone on a reload, where the player carries on as it does
// after any title.
let queue: Card[] = [];

export function playQueue(cards: Card[], shuffle = false) {
	queue = shuffle ? shuffled(cards) : cards;
	if (queue[0]) void goto(`/play/${queue[0].id}`, { state: { queued: 0 } });
}

export function queued(at: number): Card | undefined {
	return queue[at];
}

export function shuffled<T>(items: T[]): T[] {
	const out = [...items];
	for (let i = out.length - 1; i > 0; i--) {
		const j = Math.floor(Math.random() * (i + 1));
		[out[i], out[j]] = [out[j], out[i]];
	}
	return out;
}
