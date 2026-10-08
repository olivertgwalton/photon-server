import { invalidate, refreshAll } from "$app/navigation";
import { affected, type Change, changeKinds } from "./changes.js";
import { restoring } from "./restoring.svelte.js";

// A scan sends a burst of changes; the pages reload once for the burst.
const settle = 1000;

// Listens until the returned function is called. The feed keeps nothing to
// resend, so after a reconnect every page loads again.
export function connectLive(): () => void {
	const source = new EventSource("/api/v1/events");
	let reconnected = false;
	source.addEventListener("hello", () => {
		if (reconnected) refreshAll();
		reconnected = true;
	});
	const stale = new Set<string>();
	let timer: ReturnType<typeof setTimeout> | undefined;
	const take = (event: MessageEvent<string>) => {
		const change = { ...JSON.parse(event.data), kind: event.type } as Change;
		for (const key of affected(change)) stale.add(key);
		if (stale.size && !timer) {
			timer = setTimeout(() => {
				timer = undefined;
				const keys = [...stale];
				stale.clear();
				for (const key of keys) invalidate(key);
			}, settle);
		}
	};
	for (const kind of changeKinds) source.addEventListener(kind, take);
	source.addEventListener("restore.started", () => {
		restoring.on = true;
	});
	return () => {
		clearTimeout(timer);
		source.close();
	};
}
