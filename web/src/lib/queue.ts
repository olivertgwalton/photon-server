import type { components } from "#lib/api/schema.d.ts";
import { goto } from "$app/navigation";
import { shuffled } from "./shuffle.ts";

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
