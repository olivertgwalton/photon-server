import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends(keys.admin.libraries);
	const api = client(fetch);
	// Every collection, in no library: each page of them, as a server may hold
	// hundreds of a provider's box sets.
	async function every() {
		const all = [];
		for (let offset = 0, total = 1; offset < total; offset += 200) {
			const page = await need(
				api.GET("/api/v1/collections", {
					params: { query: { offset, limit: 200 } },
				}),
			);
			all.push(...page.items);
			total = page.total;
		}
		return all;
	}
	const [{ items: libraries }, plugins, collections] = await Promise.all([
		need(api.GET("/api/v1/admin/libraries")),
		need(api.GET("/api/v1/admin/plugins")),
		every(),
	]);
	// A plugin that keeps lists, as a Stremio addon's catalogs are, is a source of them too.
	const listers = plugins.items.filter((p) => p.capabilities.includes("list"));
	return { libraries, collections, listers };
};
