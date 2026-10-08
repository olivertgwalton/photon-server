import { expect, test } from "bun:test";
import {
	dayLabel,
	monthKey,
	monthOf,
	shift,
	upcoming,
	weeks,
} from "./calendar.ts";

test("a month's grid is its whole weeks, Monday first", () => {
	const october = weeks({ year: 2026, month: 10 });
	expect([october[0], october.at(-1), october.length]).toEqual([
		"2026-09-28",
		"2026-11-01",
		35,
	]);
	// A month from a Monday to a Sunday is just its own four weeks.
	expect(weeks({ year: 2027, month: 2 }).length).toBe(28);
	// Never more than six weeks, the most the server answers at once.
	expect(weeks({ year: 2026, month: 8 }).length).toBe(42);
});

test("the upcoming list runs six weeks from yesterday, its near days by name", () => {
	const days = upcoming("2026-10-08");
	expect([days[0], days[1], days.at(-1), days.length]).toEqual([
		"2026-10-07",
		"2026-10-08",
		"2026-11-17",
		42,
	]);
	expect(
		days.slice(0, 4).map((d) => dayLabel(d, "2026-10-08", "en-GB")),
	).toEqual(["Yesterday", "Today", "Tomorrow", "Saturday 10 October"]);
});

test("the month a page names, else today's, and those either side", () => {
	const today = new Date(2026, 9, 8);
	expect(monthOf("2027-01", today)).toEqual({ year: 2027, month: 1 });
	expect(monthOf("2027-13", today)).toEqual({ year: 2026, month: 10 });
	expect(monthOf(null, today)).toEqual({ year: 2026, month: 10 });
	expect(monthKey(shift({ year: 2026, month: 12 }, 1))).toBe("2027-01");
	expect(monthKey(shift({ year: 2026, month: 1 }, -1))).toBe("2025-12");
});
