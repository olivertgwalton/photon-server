import type { Shape } from "./artwork.js";

// The narrowest a card is drawn, and the space between two. A wall fits as
// many columns as these allow, as `auto-fill` does, so the page drawn by the
// server and the wall drawn by the browser line up.
const minWidth: Record<Shape, number> = { poster: 150, still: 260 };
export const columnGap = 16;

export function gridColumns(shape: Shape): string {
	return `repeat(auto-fill, minmax(${minWidth[shape]}px, 1fr))`;
}

export function columnsFor(width: number, shape: Shape): number {
	return Math.max(
		1,
		Math.floor((width + columnGap) / (minWidth[shape] + columnGap)),
	);
}
