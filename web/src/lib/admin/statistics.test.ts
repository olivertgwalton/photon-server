import { expect, test } from "bun:test";
import { buckets, dayOf } from "./statistics.ts";

const days = (n: number) =>
	Array.from({ length: n }, (_, i) => {
		const d = new Date(2026, 0, 1 + i);
		const day = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
		return { day, plays: 1, watched_ms: 60_000 };
	});

test("a day the server names is that day where the reader is", () => {
	const d = dayOf("2026-10-18");
	expect([d.getFullYear(), d.getMonth(), d.getDate()]).toEqual([2026, 9, 18]);
});

test("a month is drawn a column a day", () => {
	const out = buckets(days(30));
	expect(out).toHaveLength(30);
	expect(out[0]).toMatchObject({ days: 1, plays: 1, ms: 60_000 });
});

test("a year is drawn a column a week, the last holding what is left", () => {
	const out = buckets(days(365));
	expect(out).toHaveLength(53);
	expect(out[0]).toMatchObject({ days: 7, plays: 7, ms: 420_000 });
	expect(out.at(-1)).toMatchObject({ days: 1, plays: 1 });
	expect(out.reduce((n, b) => n + b.plays, 0)).toBe(365);
});
