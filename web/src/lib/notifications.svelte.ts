import { toast } from "svelte-sonner";
import { refreshAll } from "$app/navigation";
import { client } from "./api/client.ts";
import { problemMessage } from "./api/problem.ts";
import type { components } from "./api/schema.d.ts";

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

	// A request decided from its notification, which goes here at once: the
	// server clears it from every inbox that has it and tells each, and the
	// page of those waiting loads again. One decided by someone else first, or
	// taken back, is gone already, which is no refusal.
	async decide(n: Notification, verdict: "approve" | "decline", reason = "") {
		const r = n.request;
		if (!r) return;
		const path = { params: { path: { id: r.id } } };
		const { error, response } =
			verdict === "approve"
				? await client().POST("/api/v1/admin/requests/{id}/approve", path)
				: await client().POST("/api/v1/admin/requests/{id}/decline", {
						...path,
						body: { reason },
					});
		const gone = response.status === 404 || response.status === 409;
		if (error && !gone) {
			toast.error(problemMessage(error));
			return;
		}
		const unread = this.unread - (n.read_at ? 0 : 1);
		this.apply({ change: "cleared", cleared: [n.id], unread });
		if (gone) {
			toast.info(`${r.title} was decided already.`);
			return;
		}
		await refreshAll();
		toast.success(
			`${r.title} is ${verdict === "approve" ? "approved" : "declined"}.`,
		);
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

	// Everything shown read; one added since stays unread. A read the server
	// refused is undone by asking it again.
	async readAll() {
		const through = this.notifications[0]?.added_at;
		if (!through) return;
		this.apply({ change: "read", through, unread: 0 });
		const { error } = await client().PUT("/api/v1/profile/notifications/read", {
			body: { through },
		});
		if (error) await this.load();
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
