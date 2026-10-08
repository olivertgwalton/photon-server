import { expect, test } from "bun:test";
import type { components } from "#lib/api/schema.js";
import { point } from "./metrics";

type Own = components["schemas"]["OwnMetrics"];

function reading(at: string, sent: number, cpu: number): Own {
	return {
		at,
		playbacks: { direct: 1, remux: 0, transcode: 2 },
		transcodes: {},
		playback_starts: {},
		transcode_refusals: {},
		sent_bytes: { file: sent, segment: sent },
		cpu_seconds: cpu,
		resident_memory_bytes: 0,
		segment_wait: { buckets: [], count: 0, sum: 0 },
	};
}

test("bandwidth and CPU are rates between two readings of a node", () => {
	const before = reading("2026-10-08T12:00:00Z", 1_000, 10);
	const after = reading("2026-10-08T12:00:05Z", 3_500, 12.5);
	expect(point(before, after)).toEqual({
		at: Date.parse(after.at),
		streams: 3,
		bandwidth: 1_000,
		cpu: 50,
	});
	// A node restarted counts from nothing again: no rate, not a negative one.
	const restarted = reading("2026-10-08T12:00:10Z", 10, 0.1);
	expect(point(after, restarted).bandwidth).toBeUndefined();
	expect(point(undefined, after).cpu).toBeUndefined();
});
