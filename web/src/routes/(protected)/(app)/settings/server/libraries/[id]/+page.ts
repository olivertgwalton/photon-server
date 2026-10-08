import { error } from "@sveltejs/kit";
import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params, depends, parent }) => {
	depends(keys.admin.libraries);
	const api = client(fetch);
	const [{ server }, libraries, providers, locales] = await Promise.all([
		parent(),
		need(api.GET("/api/v1/admin/libraries")),
		need(api.GET("/api/v1/admin/providers")),
		need(api.GET("/api/v1/admin/locales")),
	]);
	const library = libraries.items.find((l) => l.id === params.id);
	if (!library) error(404, "That isn't here any more.");
	return {
		library,
		providers: providers.items,
		locales,
		serverLanguage: server.metadata_language,
		serverCountry: server.certification_country,
	};
};
