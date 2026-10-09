import { expect, test } from "bun:test";
import { adminAffected, affected, keys } from "./changes.ts";

test("a library's change reloads its wall and home, nothing else", () => {
	expect(affected({ kind: "library.changed", library_id: "l-1" })).toEqual([
		keys.home,
		keys.library("l-1"),
	]);
});

test("a title's change reloads its page and the shelves showing it", () => {
	expect(
		affected({ kind: "title.updated", title_id: "t-1", library_id: "l-1" }),
	).toEqual([keys.home, keys.title("t-1"), keys.library("l-1")]);
});

test("the profile's own marks reload every list that draws them", () => {
	expect(affected({ kind: "userdata.changed", title_id: "t-1" })).toEqual([
		keys.home,
		keys.userdata,
		keys.title("t-1"),
	]);
});

test("a code entered on a tracker reloads the profile's trackers alone", () => {
	expect(affected({ kind: "tracker.changed" })).toEqual([keys.trackers]);
});

test("a scan's progress reloads nothing: the page draws it as it comes", () => {
	expect(affected({ kind: "scan.progress", library_id: "l-1" })).toEqual([]);
});

test("an admin event reloads the dashboard page that lists it, nothing else", () => {
	expect(adminAffected("job.dead")).toEqual([keys.admin.jobs]);
	expect(adminAffected("scan.progress")).toEqual([]);
});
