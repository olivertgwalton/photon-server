import { expect, test } from "bun:test";
import { timecode } from "../format";
import { elapsed, parseClock } from "./words";

test("a marker's time reads and writes as a clock", () => {
	expect(timecode(83_000)).toBe("1:23");
	expect(timecode(3_723_000)).toBe("1:02:03");
	expect(parseClock("1:23")).toBe(83_000);
	expect(parseClock("1:02:03")).toBe(3_723_000);
	expect(parseClock("45.5")).toBe(45_500);
	expect(parseClock("1:2:3:4")).toBeUndefined();
	expect(parseClock("soon")).toBeUndefined();
});

test("how long something has run reads in its largest whole unit", () => {
	const unit = (n: number, u: "day" | "hour" | "minute" | "second") =>
		new Intl.NumberFormat(undefined, {
			style: "unit",
			unit: u,
			unitDisplay: "long",
		}).format(n);
	expect(elapsed(3 * 86_400_000 + 5 * 3_600_000)).toBe(unit(3, "day"));
	expect(elapsed(90 * 60_000)).toBe(unit(1, "hour"));
	expect(elapsed(42_000)).toBe(unit(42, "second"));
});
