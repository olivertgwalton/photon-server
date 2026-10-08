import type { components } from "#lib/api/schema.js";

type Schemas = components["schemas"];
type Own = Schemas["OwnMetrics"];

// How often the metrics page asks, and how much of what it was told it keeps
// for its sparklines.
export const pollEvery = 5_000;
export const keepFor = 10 * 60_000;

export function total(values: Record<string, number> | undefined): number {
	return Object.values(values ?? {}).reduce((a, b) => a + b, 0);
}

// One node's readings between two answers: bytes a second, CPU as a share of
// one core, and its streams. A counter that fell, the node having restarted,
// gives no rate.
export interface Point {
	at: number;
	streams: number;
	bandwidth?: number;
	cpu?: number;
}

export function point(before: Own | undefined, after: Own): Point {
	const at = Date.parse(after.at);
	const p: Point = { at, streams: total(after.playbacks) };
	if (!before) return p;
	const seconds = (at - Date.parse(before.at)) / 1000;
	const sent = total(after.sent_bytes) - total(before.sent_bytes);
	const cpu = after.cpu_seconds - before.cpu_seconds;
	if (seconds <= 0 || sent < 0 || cpu < 0) return p;
	return { ...p, bandwidth: sent / seconds, cpu: (cpu / seconds) * 100 };
}

// Each node's points over the answers kept, oldest first.
export function series(answers: Schemas["Metrics"][]): Map<string, Point[]> {
	const out = new Map<string, Point[]>();
	const last = new Map<string, Own>();
	for (const answer of answers) {
		for (const n of answer.nodes) {
			if (!n.metrics) continue;
			const points = out.get(n.id) ?? [];
			points.push(point(last.get(n.id), n.metrics));
			out.set(n.id, points);
			last.set(n.id, n.metrics);
		}
	}
	return out;
}

// An SVG path through values, the last at the right edge of width, scaled so
// the largest reaches the top. Time runs across `span` milliseconds to now.
export function sparkPath(
	points: { at: number; value: number }[],
	now: number,
	span: number,
	width: number,
	height: number,
): string {
	const top = Math.max(...points.map((p) => p.value), 0) || 1;
	return points
		.map((p, i) => {
			const x = width - ((now - p.at) / span) * width;
			const y = height - (p.value / top) * height;
			return `${i ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`;
		})
		.join(" ");
}
