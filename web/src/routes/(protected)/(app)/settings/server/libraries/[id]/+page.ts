import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params, depends }) => {
	depends("admin:libraries");
	const api = client(fetch);
	const [libraries, providers, locales, server] = await Promise.all([
		need(api.GET("/api/v1/admin/libraries")),
		need(api.GET("/api/v1/admin/providers")),
		need(api.GET("/api/v1/admin/locales")),
		need(api.GET("/api/v1/admin/server")),
	]);
	const library = libraries.items.find((l) => l.id === params.id);
	if (!library) error(404, "That isn't here any more.");
	return {
		library,
		providers: providers.items,
		locales,
		serverLanguage: server.metadata_language,
	};
};
