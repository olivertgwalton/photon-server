import type { components } from "#lib/api/schema.js";

type PartTrickplay = components["schemas"]["PartTrickplay"];

export type Thumbnail = {
	url: string;
	x: number;
	y: number;
	width: number;
	height: number;
	sheetWidth: number;
	sheetHeight: number;
};

// The thumbnail for a moment on the copy's timeline: the sheet of the part it
// falls in, and where in that sheet's grid the picture is.
export function thumbnail(
	parts: PartTrickplay[],
	ms: number,
): Thumbnail | undefined {
	const part = parts.findLast((p) => p.offset_ms <= ms) ?? parts[0];
	if (!part || part.thumbnails === 0) return undefined;
	const index = Math.min(
		Math.max(Math.floor((ms - part.offset_ms) / part.interval_ms), 0),
		part.thumbnails - 1,
	);
	const perSheet = part.columns * part.rows;
	const cell = index % perSheet;
	return {
		url: `/api/v1/parts/${part.part_id}/trickplay/${Math.floor(index / perSheet)}`,
		x: (cell % part.columns) * part.width,
		y: Math.floor(cell / part.columns) * part.height,
		width: part.width,
		height: part.height,
		sheetWidth: part.columns * part.width,
		sheetHeight: part.rows * part.height,
	};
}
