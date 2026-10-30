import { client, need } from "#lib/api/client.ts";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [server, libraries, kinds, providers, locales] = await Promise.all([
		need(api.GET("/api/v1/admin/server")),
		need(api.GET("/api/v1/admin/libraries")),
		need(api.GET("/api/v1/admin/library-kinds")),
		need(api.GET("/api/v1/admin/providers")),
		need(api.GET("/api/v1/admin/locales")),
	]);
	return {
		server,
		libraries: libraries.items,
		kinds: kinds.items,
		providers: providers.items,
		locales,
	};
};
