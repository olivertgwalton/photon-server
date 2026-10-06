import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [providers, plugins] = await Promise.all([
		need(api.GET("/api/v1/admin/providers")),
		need(api.GET("/api/v1/admin/plugins")),
	]);
	return { providers: providers.items, plugins: plugins.items };
};
