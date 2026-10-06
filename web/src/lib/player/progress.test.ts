import { afterEach, beforeEach, expect, jest, test } from "bun:test";
import { ProgressReporter, type Report, reportEvery } from "./progress.ts";

let at = 0;
let sent: Report[] = [];
let reporter: ProgressReporter;

beforeEach(() => {
	jest.useFakeTimers();
	at = 0;
	sent = [];
	reporter = new ProgressReporter(
		() => at,
		(r) => {
			sent.push(r);
			return undefined;
		},
	);
});

afterEach(() => jest.useRealTimers());

test("a playing title says where it is on a steady beat", () => {
	reporter.playing();
	at = 9_000;
	jest.advanceTimersByTime(reportEvery);
	at = 19_400.6;
	jest.advanceTimersByTime(reportEvery);
	expect(sent).toEqual([
		{ kind: "progress", position_ms: 0, state: "playing" },
		{ kind: "progress", position_ms: 9_000, state: "playing" },
		{ kind: "progress", position_ms: 19_401, state: "playing" },
	]);
});

test("playing again after a stall says nothing new", () => {
	reporter.playing();
	at = 3_000;
	reporter.playing();
	expect(sent).toHaveLength(1);
});

test("a pause is said at once and the beat stops until it plays again", () => {
	reporter.playing();
	at = 4_000;
	reporter.paused();
	jest.advanceTimersByTime(reportEvery * 3);
	expect(sent.at(-1)).toEqual({
		kind: "progress",
		position_ms: 4_000,
		state: "paused",
	});
	expect(sent).toHaveLength(2);
});

test("a seek is said at once, in the state it is in", () => {
	reporter.playing();
	at = 600_000;
	reporter.seeked();
	expect(sent.at(-1)).toEqual({
		kind: "progress",
		position_ms: 600_000,
		state: "playing",
	});
});

test("the stop carries the final position, once, and nothing follows it", () => {
	reporter.playing();
	at = 42_000;
	reporter.stop(true);
	reporter.stop();
	reporter.seeked();
	jest.advanceTimersByTime(reportEvery * 2);
	expect(sent.at(-1)).toEqual({
		kind: "stop",
		position_ms: 42_000,
		keepalive: true,
	});
	expect(sent.filter((r) => r.kind === "stop")).toHaveLength(1);
	expect(sent).toHaveLength(2);
});
