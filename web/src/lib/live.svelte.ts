import { invalidate, refreshAll } from "$app/navigation";
import { affected, type Change, changeKinds } from "./changes.js";
import { restoring } from "./restoring.svelte.js";

// A scan sends a burst of changes; the pages reload once for the burst.
const settle = 1000;

// A refused or dropped stream is not retried by the browser once it has
// closed, so it is opened again after this long.
export const retryAfter = 5_000;

// Reloads what a stream's events make stale, once the burst has settled.
export function invalidateLater() {
	const stale = new Set<string>();
	let timer: ReturnType<typeof setTimeout> | undefined;
	return {
		add(keys: string[]) {
			for (const key of keys) stale.add(key);
			if (stale.size && !timer) {
				timer = setTimeout(() => {
					timer = undefined;
					const keys = [...stale];
					stale.clear();
					for (const key of keys) invalidate(key);
				}, settle);
			}
		},
		stop() {
			clearTimeout(timer);
		},
	};
}

// Listens until the returned function is called. The feed keeps nothing to
// resend, so after a reconnect every page loads again.
export function connectLive(): () => void {
	let source: EventSource | undefined;
	let retry: ReturnType<typeof setTimeout> | undefined;
	let reconnected = false;
	const later = invalidateLater();
	const take = (event: MessageEvent<string>) => {
		const change = { ...JSON.parse(event.data), kind: event.type } as Change;
		later.add(affected(change));
	};
	const connect = () => {
		source = new EventSource("/api/v1/events");
		source.addEventListener("hello", () => {
			if (reconnected) refreshAll();
			reconnected = true;
		});
		for (const kind of changeKinds) source.addEventListener(kind, take);
		source.addEventListener("restore.started", () => {
			restoring.on = true;
		});
		source.onerror = () => {
			if (source?.readyState === EventSource.CLOSED) {
				retry = setTimeout(connect, retryAfter);
			}
		};
	};
	connect();
	return () => {
		clearTimeout(retry);
		later.stop();
		source?.close();
	};
}
