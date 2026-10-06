import type { components } from "#lib/api/schema.js";

type PlayState = components["schemas"]["PlayState"];

export type Report =
	| { kind: "progress"; position_ms: number; state: PlayState }
	| { kind: "stop"; position_ms: number; keepalive: boolean };

// How often a playing title says where it is, as Jellyfin's web player does.
export const reportEvery = 10_000;

// Tells the server where a playback has got to: on a steady beat while it
// plays, at once on a pause, a resume or a seek, and once, finally, on a stop.
// Nothing is said after the stop.
export class ProgressReporter {
	#timer: ReturnType<typeof setInterval> | undefined;
	#state: PlayState = "paused";
	#stopped = false;

	constructor(
		private readonly position: () => number,
		private readonly send: (report: Report) => Promise<unknown> | undefined,
	) {}

	// Playing again after a stall or a seek is not news.
	playing() {
		if (this.#state === "playing") return;
		this.#state = "playing";
		this.#progress();
		clearInterval(this.#timer);
		this.#timer = setInterval(() => this.#progress(), reportEvery);
	}

	paused() {
		this.#state = "paused";
		clearInterval(this.#timer);
		this.#progress();
	}

	seeked() {
		this.#progress();
	}

	// keepalive lets the stop outlive a page being closed.
	stop(keepalive = false): Promise<unknown> | undefined {
		if (this.#stopped) return;
		this.#stopped = true;
		clearInterval(this.#timer);
		return this.send({ kind: "stop", position_ms: this.#at(), keepalive });
	}

	#progress() {
		if (this.#stopped) return;
		this.send({
			kind: "progress",
			position_ms: this.#at(),
			state: this.#state,
		});
	}

	#at() {
		return Math.max(0, Math.round(this.position()));
	}
}
