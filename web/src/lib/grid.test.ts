import { expect, test } from "bun:test";
import { columnsFor, minCardWidth } from "./grid.ts";

// An iPhone 16 Pro Max is 440 wide; the page keeps 12 either side.
const phone = 440 - 24;

test("a phone's wall is three posters across, four compact and two large, as the apps' are", () => {
	const across = (size: "compact" | "standard" | "large") =>
		columnsFor(phone, minCardWidth("poster", size, true));
	expect(across("compact")).toBe(4);
	expect(across("standard")).toBe(3);
	expect(across("large")).toBe(2);
});

test("a wider window draws its cards larger than a phone does", () => {
	expect(minCardWidth("poster", "standard", false)).toBeGreaterThan(
		minCardWidth("poster", "standard", true),
	);
	expect(columnsFor(1000, minCardWidth("still", "standard", false))).toBe(3);
});
