import { invalidate, refreshAll } from "$app/navigation";
import { affected, type Change, changeKinds } from "./changes.js";
import { restoring } from "./restoring.svelte.js";

// A scan sends a burst of changes; the pages reload once for the burst.
const settle = 1000;

// A refused or dropped stream is not retried by the browser once it has
// closed, so it is opened again after this long.
const retryAfter = 5_000;

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

type Listeners = Record<string, (event: MessageEvent<string>) => void>;

// Opens a stream with these listeners and keeps it open until the returned
// function is called. `dropped` is told of every error, closing or not.
export function openStream(
	url: string,
	listeners: Listeners,
	dropped?: () => void,
): () => void {
	let source: EventSource | undefined;
	let retry: ReturnType<typeof setTimeout> | undefined;
	const connect = () => {
		source = new EventSource(url);
		for (const [name, listener] of Object.entries(listeners)) {
			source.addEventListener(name, listener);
		}
		source.onerror = () => {
			dropped?.();
			if (source?.readyState === EventSource.CLOSED) {
				retry = setTimeout(connect, retryAfter);
			}
		};
	};
	connect();
	return () => {
		clearTimeout(retry);
		source?.close();
	};
}

// Listens until the returned function is called. The feed keeps nothing to
// resend, so after a reconnect every page loads again.
export function connectLive(): () => void {
	let reconnected = false;
	const later = invalidateLater();
	const take = (event: MessageEvent<string>) => {
		const change = { ...JSON.parse(event.data), kind: event.type } as Change;
		later.add(affected(change));
	};
	const close = openStream("/api/v1/events", {
		hello: () => {
			if (reconnected) refreshAll();
			reconnected = true;
		},
		...Object.fromEntries(changeKinds.map((kind) => [kind, take])),
		"restore.started": () => {
			restoring.on = true;
		},
	});
	return () => {
		later.stop();
		close();
	};
}
