import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, parent }) => {
	const api = client(fetch);
	const [devices, profiles, { me }] = await Promise.all([
		need(api.GET("/api/v1/auth/devices")),
		need(api.GET("/api/v1/profiles")),
		parent(),
	]);
	const mine = profiles.items.find((p) => p.id === me.id);
	return { devices: devices.items, lock: mine?.lock ?? "none" };
};
