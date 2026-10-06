import { expect, test } from "bun:test";
import { timecode } from "../format";
import { describe, parseClock } from "./words";

test("a marker's time reads and writes as a clock", () => {
	expect(timecode(83_000)).toBe("1:23");
	expect(timecode(3_723_000)).toBe("1:02:03");
	expect(parseClock("1:23")).toBe(83_000);
	expect(parseClock("1:02:03")).toBe(3_723_000);
	expect(parseClock("45.5")).toBe(45_500);
	expect(parseClock("1:2:3:4")).toBeUndefined();
	expect(parseClock("soon")).toBeUndefined();
});

test("an event reads as who did what", () => {
	const names = {
		profiles: new Map([["p-ada", "Ada"]]),
		libraries: new Map([["l-films", "Films"]]),
	};
	expect(
		describe(
			{
				kind: "playback.stopped",
				at: "2026-10-06T20:00:00Z",
				details: {
					reach: "end",
					playback: {
						profile: { id: "p-ada", name: "Ada" },
						title: {
							id: "t",
							kind: "episode",
							title: "Pilot",
							show: "Small Show",
							season_number: 1,
							episode_number: 2,
						},
					},
				},
			},
			names,
		),
	).toBe("Ada finished Small Show S1 E2 · Pilot");
	expect(
		describe(
			{
				kind: "library.titles_added",
				at: "2026-10-06T20:00:00Z",
				library_id: "l-films",
				details: { titles: 1 },
			},
			names,
		),
	).toBe("1 title was added to Films");
});
