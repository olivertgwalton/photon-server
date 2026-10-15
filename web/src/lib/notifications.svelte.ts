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

	// Those added replace any of their ids, at the top; those read are marked.
	apply(change: InboxChange) {
		this.unread = change.unread;
		const added = change.notifications ?? [];
		if (added.length) {
			const ids = new Set(added.map((n) => n.id));
			this.notifications = [
				...added,
				...this.notifications.filter((n) => !ids.has(n.id)),
			];
		}
		const read = new Set(change.read);
		const through = change.through && Date.parse(change.through);
		const now = new Date().toISOString();
		for (const n of this.notifications) {
			if (read.has(n.id) || (through && Date.parse(n.added_at) <= through))
				n.read_at ??= now;
		}
	}

	// Read at once here; the server tells the profile's other devices.
	read(n: Notification) {
		if (n.read_at) return;
		this.apply({ change: "read", read: [n.id], unread: this.unread - 1 });
		client().PUT("/api/v1/profile/notifications/{id}/read", {
			params: { path: { id: n.id } },
		});
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
