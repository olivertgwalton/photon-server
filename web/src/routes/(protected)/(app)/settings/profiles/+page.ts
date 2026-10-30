import { client, need } from "#lib/api/client.ts";
import { keys } from "#lib/changes.ts";
import type { PageLoad } from "./$types";

// An admin sees every profile; a manager, the ones it keeps.
export const load: PageLoad = async ({ fetch, depends, parent }) => {
	depends(keys.admin.profiles);
	const api = client(fetch);
	const [{ me }, profiles, devices] = await Promise.all([
		parent(),
		need(api.GET("/api/v1/profiles")),
		need(api.GET("/api/v1/devices")),
	]);
	if (me.role !== "admin") {
		profiles.items = profiles.items.filter((p) => p.manager === me.id);
	}
	// When each was last seen, of the devices of the profiles this one keeps.
	const seen = new Map<string, string>();
	for (const d of devices.items) {
		const before = seen.get(d.profile_id);
		if (!before || d.last_seen_at > before)
			seen.set(d.profile_id, d.last_seen_at);
	}
	return {
		profiles: profiles.items.map((p) => ({ ...p, seen: seen.get(p.id) })),
	};
};
