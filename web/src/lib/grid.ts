import type { Shape } from "./artwork.js";

// How large a wall draws its cards, as the apps' Card Size has it.
export type CardSize = "compact" | "standard" | "large";
export const cardSizes: CardSize[] = ["compact", "standard", "large"];

// The narrowest a card is drawn at Standard, on a phone and on anything wider,
// and how far each size takes it: the apps' own widths, so a wall is as many
// across as theirs. Compact takes a poster further than a still, adding a
// column on every phone.
const standard: Record<Shape, { phone: number; wider: number }> = {
	poster: { phone: 108, wider: 132 },
	still: { phone: 165, wider: 320 },
};
const scale: Record<Shape, Record<CardSize, number>> = {
	poster: { compact: 0.72, standard: 1, large: 1.3 },
	still: { compact: 0.87, standard: 1, large: 1.3 },
};
export const columnGap = 12;

// How far each size takes a row's cards, which are counted in thirds of a
// card more or less rather than whole columns: Large is a step smaller than a
// wall's, as the apps', so a wide row of posters goes from six to five.
export const railScale: Record<CardSize, number> = {
	compact: 0.87,
	standard: 1,
	large: 1.2,
};

export function minCardWidth(
	shape: Shape,
	size: CardSize,
	phone: boolean,
): number {
	const width = standard[shape][phone ? "phone" : "wider"];
	return Math.round(width * scale[shape][size]);
}

// A wall fits as many columns as the width allows, as `auto-fill` does, so the
// rows the browser lays out and the rows the wall reckons with line up.
export function gridColumns(min: number): string {
	return `repeat(auto-fill, minmax(${min}px, 1fr))`;
}

export function columnsFor(width: number, min: number): number {
	return Math.max(1, Math.floor((width + columnGap) / (min + columnGap)));
}
