import type { components } from "#lib/api/schema.d.ts";

type Day = components["schemas"]["DayWatched"];

// One column of the chart: a day, or a week where the span is too long for a
// column a day to be readable.
export type Bucket = { from: Date; days: number; plays: number; ms: number };

// The longest span drawn a column a day: a quarter. A year's 365 columns would
// be thinner than the gaps between them.
const dailyUpTo = 92;

// A day the server names, "2026-10-18", as that day where the reader is: read
// as UTC it would be the evening before for anyone west of Greenwich.
export function dayOf(day: string): Date {
	const [y, m, d] = day.split("-").map(Number);
	return new Date(y, m - 1, d);
}

// The report's days as the chart's columns, oldest first: each day, or each
// week from the first day on, the last week holding what is left of the span.
export function buckets(daily: Day[]): Bucket[] {
	const size = daily.length > dailyUpTo ? 7 : 1;
	const out: Bucket[] = [];
	for (let i = 0; i < daily.length; i += size) {
		const run = daily.slice(i, i + size);
		out.push({
			from: dayOf(run[0].day),
			days: run.length,
			plays: run.reduce((n, d) => n + d.plays, 0),
			ms: run.reduce((n, d) => n + d.watched_ms, 0),
		});
	}
	return out;
}
