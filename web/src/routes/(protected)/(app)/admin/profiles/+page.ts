import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends("admin:profiles");
	const api = client(fetch);
	const [profiles, devices] = await Promise.all([
		need(api.GET("/api/v1/profiles")),
		need(api.GET("/api/v1/auth/devices")),
	]);
	// When each was last seen: devices name their profile, not its id.
	const seen = new Map<string, string>();
	for (const d of devices.items) {
		const before = seen.get(d.profile);
		if (!before || d.last_seen_at > before) seen.set(d.profile, d.last_seen_at);
	}
	return {
		profiles: profiles.items.map((p) => ({ ...p, seen: seen.get(p.name) })),
	};
};
