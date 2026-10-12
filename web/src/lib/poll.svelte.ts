// How often a page reads a server's job again to show how far it has got.
export const progressEvery = 2_000;

// Calls `reload` every `every` milliseconds while `running` holds: for work
// that says how far it has got only when asked.
export function pollWhile(
	running: () => boolean,
	reload: () => unknown,
	every: number,
) {
	$effect(() => {
		if (!running()) return;
		const timer = setInterval(reload, every);
		return () => clearInterval(timer);
	});
}
