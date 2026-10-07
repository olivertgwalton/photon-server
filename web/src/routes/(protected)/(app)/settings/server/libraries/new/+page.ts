import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [providers, locales, server] = await Promise.all([
		need(api.GET("/api/v1/admin/providers")),
		need(api.GET("/api/v1/admin/locales")),
		need(api.GET("/api/v1/admin/server")),
	]);
	return {
		providers: providers.items,
		locales,
		serverLanguage: server.metadata_language,
	};
};
