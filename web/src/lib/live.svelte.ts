import { invalidate, refreshAll } from "$app/navigation";
import type { LiveStream } from "./admin/stream.svelte.ts";
import type { components } from "./api/schema.d.ts";
import {
	adminAffected,
	affected,
	type Change,
	inboxAffected,
} from "./changes.ts";
import { inbox } from "./notifications.svelte.ts";
import { restoring } from "./restoring.svelte.ts";
import { settling } from "./settle.ts";

// A dropped socket is opened again after this long.
const retryAfter = 5_000;

type Feed = components["schemas"]["Feed"];

// One message of the server's events socket: an event of a feed, or the
// feed's first, which says what is going on now.
type Message = { feed: Feed; event: string; data: unknown };

// Opens the server's events socket and keeps it open, opening it again
// whenever it drops, until the returned function is called. `dropped` is told
// each time it drops.
function openSocket(
	feed: Feed,
	heard: (message: Message) => void,
	dropped: () => void,
): () => void {
	const url = new URL(`/api/v1/events?feed=${feed}`, location.href);
	url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
	let socket: WebSocket | undefined;
	let retry: ReturnType<typeof setTimeout> | undefined;
	let closed = false;
	const connect = () => {
		socket = new WebSocket(url);
		socket.onmessage = (e: MessageEvent<string>) => heard(JSON.parse(e.data));
		socket.onclose = () => {
			if (closed) return;
			dropped();
			retry = setTimeout(connect, retryAfter);
		};
	};
	connect();
	return () => {
		closed = true;
		clearTimeout(retry);
		socket?.close();
	};
}

// Listens until the returned function is called: to what changes of what the
// profile sees, and, given an admin's LiveStream, to what the server is doing.
// Nothing is kept to resend, so after a reconnect every page loads again.
export function connectLive(server?: LiveStream): () => void {
	let reconnected = false;
	// Reloads what the socket's events make stale, once the burst has settled.
	const later = settling((keys) => {
		for (const key of keys) invalidate(key);
	});
	const close = openSocket(
		server ? "server" : "profile",
		({ feed, event, data }) => {
			if (feed === "server") {
				server?.hear(event, data);
				later.add(adminAffected(event));
				return;
			}
			switch (event) {
				case "hello":
					if (reconnected) {
						refreshAll();
						inbox.load();
					}
					reconnected = true;
					break;
				case "notifications.changed": {
					const change = data as components["schemas"]["InboxChange"];
					inbox.apply(change);
					later.add(inboxAffected(change));
					break;
				}
				case "restore.started":
					restoring.on = true;
					break;
				default:
					later.add(affected({ ...(data as Change), kind: event }));
			}
		},
		() => server?.drop(),
	);
	return () => {
		later.stop();
		close();
	};
}
