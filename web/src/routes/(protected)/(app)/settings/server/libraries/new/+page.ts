import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, parent }) => {
	const api = client(fetch);
	const [{ server }, providers, locales] = await Promise.all([
		parent(),
		need(api.GET("/api/v1/admin/providers")),
		need(api.GET("/api/v1/admin/locales")),
	]);
	return {
		providers: providers.items,
		locales,
		serverLanguage: server.metadata_language,
	};
};
