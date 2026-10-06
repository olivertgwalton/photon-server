// The time now, moved on every `every` milliseconds while a component that
// reads it is mounted: for positions and "a minute ago" that keep up.
export function ticking(every = 1_000) {
	let now = $state(Date.now());
	$effect(() => {
		const timer = setInterval(() => {
			now = Date.now();
		}, every);
		return () => clearInterval(timer);
	});
	return {
		get now() {
			return now;
		},
	};
}
