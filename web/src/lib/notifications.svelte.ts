import { client } from "./api/client.js";
import type { components } from "./api/schema.js";

type Notification = components["schemas"]["Notification"];
type InboxChange = components["schemas"]["InboxChange"];

// The profile's inbox, as the bell draws it: loaded once as the shell opens,
// then changed by what the server tells of it, from this device or another.
class Inbox {
	notifications = $state<Notification[]>([]);
	unread = $state(0);

	async load() {
		const { data } = await client().GET("/api/v1/profile/notifications");
		if (!data) return;
		this.notifications = data.items;
		this.unread = data.unread;
	}

	// Those added replace any of their ids, at the top; those read are marked;
	// those cleared, by id or added by `through`, leave.
	apply(change: InboxChange) {
		this.unread = change.unread;
		if (change.change === "cleared") {
			const ids = new Set(change.cleared);
			const through = Date.parse(change.through ?? "");
			this.notifications = this.notifications.filter(
				(n) => !ids.has(n.id) && !(Date.parse(n.added_at) <= through),
			);
			return;
		}
		const added = change.notifications ?? [];
		if (added.length) {
			const ids = new Set(added.map((n) => n.id));
			this.notifications = [
				...added,
				...this.notifications.filter((n) => !ids.has(n.id)),
			];
		}
		const through = change.through && Date.parse(change.through);
		const now = new Date().toISOString();
		for (const n of this.notifications) {
			if (through && Date.parse(n.added_at) <= through) n.read_at ??= now;
		}
	}

	// Opened, so gone here at once; the server tells the profile's other
	// devices. One not listed yet, as on a page a push opened, is asked for
	// again once cleared, so a list loaded meanwhile does not bring it back.
	async clear(id: number) {
		const n = this.notifications.find((n) => n.id === id);
		if (n) {
			const unread = this.unread - (n.read_at ? 0 : 1);
			this.apply({ change: "cleared", cleared: [id], unread });
		}
		const { error } = await client().DELETE(
			"/api/v1/profile/notifications/{id}",
			{ params: { path: { id } } },
		);
		if (error || !n) await this.load();
	}

	// Everything shown cleared from the inbox, here at once and then on the
	// server, which tells the profile's other devices; one added since stays.
	// A clear the server refused is undone by asking it again.
	async clearAll() {
		const through = this.notifications[0]?.added_at;
		if (!through) return;
		this.apply({ change: "cleared", through, unread: 0 });
		const { error } = await client().PUT(
			"/api/v1/profile/notifications/cleared",
			{
				body: { through },
			},
		);
		if (error) await this.load();
	}

	// Everything shown read; one added since stays unread.
	readAll() {
		const through = this.notifications[0]?.added_at;
		if (!through) return;
		this.apply({ change: "read", through, unread: 0 });
		client().PUT("/api/v1/profile/notifications/read", { body: { through } });
	}
}

export const inbox = new Inbox();

// This browser's push subscription, sent again as the app opens: one its
// browser renewed with no page open to say so, as Firefox does, is pushed to
// from then. The server answers one it has already as it was.
export async function keepPushing() {
	if (
		typeof Notification === "undefined" ||
		Notification.permission !== "granted"
	)
		return;
	const reg = await navigator.serviceWorker.ready;
	const sub = await reg.pushManager.getSubscription();
	if (!sub) return;
	await client().POST("/api/v1/profile/push-subscriptions", {
		body: sub.toJSON() as components["schemas"]["Subscribe"],
	});
}
