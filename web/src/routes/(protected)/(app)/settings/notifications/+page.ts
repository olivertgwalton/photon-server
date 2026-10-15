import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [prefs, subscriptions, push] = await Promise.all([
		need(api.GET("/api/v1/profile/preferences")),
		need(api.GET("/api/v1/profile/push-subscriptions")),
		need(api.GET("/api/v1/push-key")),
	]);
	return { prefs, subscriptions: subscriptions.items, key: push.key };
};
