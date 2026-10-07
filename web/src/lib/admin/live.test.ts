import { describe, expect, test } from "bun:test";
import { apply, idle, type Live, type NowPlaying, positionAt } from "./live";

const playing: NowPlaying = {
	id: "pb-1",
	profile: { id: "p-ada", name: "Ada" },
	device: {
		id: "d-1",
		name: "Living Room",
		client: "Photon",
		address: "10.0.0.2",
	},
	title: { id: "t-film", kind: "movie", title: "Quiet Hours" },
	version: { id: "v-1", container: "mkv", duration_ms: 6_000_000 },
	method: "direct",
	state: "playing",
	position_ms: 60_000,
	started_at: "2026-10-06T20:00:00Z",
	updated_at: "2026-10-06T20:01:00Z",
	node_id: "n-1",
};

const at = "2026-10-06T20:02:00Z";

function after(...events: [string, unknown][]): Live {
	return events.reduce((live, [name, data]) => apply(live, name, data), idle);
}

describe("the admin event stream", () => {
	test("starts from its snapshot, and a reconnect's snapshot replaces it", () => {
		const first = after([
			"snapshot",
			{ tasks: [], jobs: [], backlogs: [], scans: [], playbacks: [playing] },
		]);
		expect(first.ready).toBe(true);
		expect(first.playbacks).toEqual([playing]);
		const again = apply(first, "snapshot", {
			tasks: [],
			jobs: [],
			backlogs: [],
			scans: [],
			playbacks: [],
		});
		expect(again.playbacks).toEqual([]);
	});

	test("follows a play from its start through a pause to its stop", () => {
		const started = after([
			"playback.started",
			{ kind: "playback.started", at, details: { playback: playing } },
		]);
		expect(started.playbacks).toHaveLength(1);
		expect(started.arrived).toHaveLength(1);

		const paused = apply(started, "playback.paused", {
			kind: "playback.paused",
			at,
			details: { playback: { ...playing, state: "paused" } },
		});
		expect(paused.playbacks[0]?.state).toBe("paused");
		// A pause is not something the activity log keeps.
		expect(paused.arrived).toHaveLength(1);

		const stopped = apply(paused, "playback.stopped", {
			kind: "playback.stopped",
			at,
			details: { playback: playing, reach: "end" },
		});
		expect(stopped.playbacks).toEqual([]);
		expect(stopped.arrived).toHaveLength(2);
	});

	test("grows a scan's progress as the walk finds folders, and ends it", () => {
		const progress = (done: number, known: number) =>
			[
				"scan.progress",
				{
					kind: "scan.progress",
					at,
					library_id: "l-films",
					details: { phase: "reading", done, known },
				},
			] as [string, unknown];
		const live = after(progress(2, 10), progress(8, 40));
		expect(live.scans).toEqual([
			{ library_id: "l-films", phase: "reading", done: 8, known: 40 },
		]);
		const scanned = apply(live, "library.scanned", {
			kind: "library.scanned",
			at,
			library_id: "l-films",
			details: { folders: 40, probed: 3 },
		});
		expect(scanned.scans).toEqual([]);
	});

	test("ends a scan whose job failed, which tells no library.scanned", () => {
		const live = after(
			[
				"scan.progress",
				{
					kind: "scan.progress",
					at,
					library_id: "l-films",
					details: { phase: "reading", done: 1, known: 2 },
				},
			],
			[
				"job.failed",
				{
					kind: "job.failed",
					at,
					library_id: "l-films",
					details: { job_id: 7, job_kind: "scan_library", error: "gone" },
				},
			],
		);
		expect(live.scans).toEqual([]);
	});

	test("lists tasks and jobs while they run", () => {
		const running = after(
			[
				"task.started",
				{ kind: "task.started", at, details: { task: "scan_libraries" } },
			],
			[
				"job.started",
				{
					kind: "job.started",
					at,
					title_id: "t-film",
					details: {
						job_id: 3,
						job_kind: "identify",
						subject: "t-film",
						attempt: 1,
					},
				},
			],
		);
		expect(running.tasks).toEqual([{ key: "scan_libraries", started_at: at }]);
		expect(running.jobs[0]).toMatchObject({
			id: 3,
			kind: "identify",
			title_id: "t-film",
		});

		const done = apply(
			apply(running, "task.finished", {
				kind: "task.finished",
				at,
				details: { task: "scan_libraries", result: "succeeded" },
			}),
			"job.finished",
			{
				kind: "job.finished",
				at,
				details: { job_id: 3, job_kind: "identify" },
			},
		);
		expect(done.tasks).toEqual([]);
		expect(done.jobs).toEqual([]);
	});

	test("counts a backlog down and drops it once none is left", () => {
		const progress = (left: number, done: number): [string, unknown] => [
			"jobs.progress",
			{
				kind: "jobs.progress",
				at,
				details: { job_kind: "previews", left, done },
			},
		];
		const started = after(
			[
				"snapshot",
				{
					tasks: [],
					jobs: [],
					backlogs: [{ kind: "identify", left: 2, done: 0 }],
					scans: [],
					playbacks: [],
				},
			],
			progress(8_022, 585),
			progress(8_021, 586),
		);
		expect(started.backlogs).toEqual([
			{ kind: "identify", left: 2, done: 0 },
			{ kind: "previews", left: 8_021, done: 586 },
		]);
		expect(apply(started, ...progress(0, 8_607)).backlogs).toEqual([
			{ kind: "identify", left: 2, done: 0 },
		]);
	});

	test("forgets a kind's running jobs once its backlog is stopped", () => {
		const job = (id: number, kind: string) => ({
			id,
			kind,
			subject: `s-${id}`,
			attempt: 1,
		});
		const stopped = after(
			[
				"snapshot",
				{
					tasks: [],
					jobs: [job(1, "previews"), job(2, "identify")],
					backlogs: [{ kind: "previews", left: 1_478, done: 2 }],
					scans: [],
					playbacks: [],
				},
			],
			[
				"jobs.progress",
				{
					kind: "jobs.progress",
					at,
					details: { job_kind: "previews", left: 0, done: 0 },
				},
			],
		);
		expect(stopped.backlogs).toEqual([]);
		expect(stopped.jobs.map((j) => j.kind)).toEqual(["identify"]);
	});
});

describe("a playing title's position", () => {
	test("moves on with the clock while it plays, and no further than its end", () => {
		const updated = Date.parse(playing.updated_at);
		expect(positionAt(playing, updated + 5_000)).toBe(65_000);
		expect(positionAt(playing, updated + 10_000_000)).toBe(6_000_000);
		expect(positionAt({ ...playing, state: "paused" }, updated + 5_000)).toBe(
			60_000,
		);
	});
});
