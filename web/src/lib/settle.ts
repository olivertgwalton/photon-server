// A scan sends a burst of changes. The pages reload once it has gone quiet for
// settle, and at most settleAtMost after its first change while it runs on, so a
// long scan still shows what it finds without a wall going back to its first page
// every second.
export const settle = 1000;
export const settleAtMost = 10_000;

// Gathers the keys changes make stale, and hands them to reload once the burst
// they come in has settled.
export function settling(reload: (keys: string[]) => void) {
	const stale = new Set<string>();
	let quiet: ReturnType<typeof setTimeout> | undefined;
	let due: ReturnType<typeof setTimeout> | undefined;
	function stop() {
		clearTimeout(quiet);
		clearTimeout(due);
		quiet = due = undefined;
	}
	function flush() {
		stop();
		const keys = [...stale];
		stale.clear();
		reload(keys);
	}
	return {
		add(keys: string[]) {
			// An event that makes nothing stale, as a scan's progress, is no part of a burst.
			if (!keys.length) return;
			for (const key of keys) stale.add(key);
			clearTimeout(quiet);
			quiet = setTimeout(flush, settle);
			due ??= setTimeout(flush, settleAtMost);
		},
		stop,
	};
}
