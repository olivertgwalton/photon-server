import { client, need } from "#lib/api/client.ts";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, parent }) => {
	const api = client(fetch);
	const [{ server }, kinds, providers, locales] = await Promise.all([
		parent(),
		need(api.GET("/api/v1/admin/library-kinds")),
		need(api.GET("/api/v1/admin/providers")),
		need(api.GET("/api/v1/admin/locales")),
	]);
	return {
		kinds: kinds.items,
		providers: providers.items,
		locales,
		serverLanguage: server.metadata_language,
		serverCountry: server.certification_country,
	};
};
