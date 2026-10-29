import { MediaQuery } from "svelte/reactivity";
import type { Shape } from "./artwork.js";
import { type CardSize, cardSizes, minCardWidth } from "./grid.js";

// The size chosen, kept in this browser as the apps keep it on each device.
const key = "card-size";

function read(): CardSize {
	try {
		const kept = localStorage.getItem(key);
		if (cardSizes.includes(kept as CardSize)) return kept as CardSize;
	} catch {}
	return "standard";
}

const chosen = $state({ size: read() });
// A phone held upright, as the apps' compact width is.
const phone = new MediaQuery("max-width: 639px");

export function cardSize(): CardSize {
	return chosen.size;
}

export function chooseCardSize(size: CardSize) {
	chosen.size = size;
	localStorage.setItem(key, size);
}

// The narrowest a wall draws a card of this shape now.
export function cardMin(shape: Shape): number {
	return minCardWidth(shape, chosen.size, phone.current);
}
