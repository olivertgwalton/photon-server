/// <reference no-default-lib="true"/>
/// <reference lib="esnext" />
/// <reference lib="webworker" />

// Shows the notifications the server pushes, in browsers that do not show a
// Declarative Web Push message themselves (Safari does), and opens a title's
// page when one is clicked. It handles no fetch: the app loads from the
// network as it would without it.
const sw = self as unknown as ServiceWorkerGlobalScope;

type Declarative = {
	notification: NotificationOptions & { title: string; navigate: string };
};

sw.addEventListener("install", () => sw.skipWaiting());
sw.addEventListener("activate", (event) => event.waitUntil(sw.clients.claim()));

sw.addEventListener("push", (event) => {
	const message = event.data?.json() as Declarative | undefined;
	if (!message) return;
	const { title, navigate, ...options } = message.notification;
	event.waitUntil(
		sw.registration.showNotification(title, {
			icon: "/favicon.svg",
			...options,
			data: { navigate },
		}),
	);
});

sw.addEventListener("notificationclick", (event) => {
	event.notification.close();
	const url = new URL(
		event.notification.data?.navigate ?? "/",
		sw.registration.scope,
	);
	if (url.origin !== sw.location.origin) return;
	event.waitUntil(
		(async () => {
			for (const w of await sw.clients.matchAll({ type: "window" })) {
				const shown = await w.navigate(url).catch(() => null);
				if (shown) return void (await shown.focus());
			}
			await sw.clients.openWindow(url);
		})(),
	);
});

// A subscription the browser renews is sent to the server; one renewed where
// the event carries no new subscription is sent as the app next opens.
sw.addEventListener("pushsubscriptionchange", (event) => {
	const renewed = (event as Event & { newSubscription?: PushSubscription })
		.newSubscription;
	if (!renewed) return;
	(event as ExtendableEvent).waitUntil(
		fetch("/api/v1/profile/push-subscriptions", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(renewed.toJSON()),
		}),
	);
});
