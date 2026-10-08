import { expect, test } from "bun:test";
import { withQuery } from "./address.ts";

test("a page's address keeps what it asks but what changes", () => {
	const here = new URL("http://photon.test/calendar?view=month&filter=all");
	expect(withQuery(here, { month: "2026-11" })).toBe(
		"/calendar?view=month&filter=all&month=2026-11",
	);
	expect(withQuery(here, { view: undefined, filter: "" })).toBe("/calendar");
});
