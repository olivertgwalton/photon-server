// How often the app asks whether a node answers again.
const pollEvery = 2_000;
// A node may already be back before the app saw it gone; this long answering
// is taken as back.
const upFor = 30_000;

export type Ready = () => Promise<boolean>;

const readyz: Ready = async () => {
	try {
		return (await fetch("/readyz", { cache: "no-store" })).ok;
	} catch {
		return false;
	}
};

// Resolves once a node answers ready after the restore: seen down then up, or
// up for upFor, which a restore quicker than the polls leaves.
export async function backAfterRestore(
	ready: Ready = readyz,
	wait = (ms: number) => new Promise((r) => setTimeout(r, ms)),
	now = () => Date.now(),
): Promise<void> {
	let wentDown = false;
	const since = now();
	for (;;) {
		await wait(pollEvery);
		const up = await ready();
		if (!up) wentDown = true;
		else if (wentDown || now() - since >= upFor) return;
	}
}
