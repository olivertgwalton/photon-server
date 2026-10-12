import { createSubscriber } from "svelte/reactivity";

// The time now, moved on every `every` milliseconds while something reads it:
// for positions and "a minute ago" that keep up.
export class Clock {
	#subscribe;

	constructor(every = 1_000) {
		this.#subscribe = createSubscriber((update) => {
			const timer = setInterval(update, every);
			return () => clearInterval(timer);
		});
	}

	get now() {
		this.#subscribe();
		return Date.now();
	}
}
