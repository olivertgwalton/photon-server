import { client, need } from "#lib/api/client.ts";
import { keys } from "#lib/changes.ts";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params, depends }) => {
	// A field unlocked takes its sources' at the next scan, and shows as it does.
	depends(keys.title(params.id));
	const api = client(fetch);
	const [title, providers, locales] = await Promise.all([
		need(
			api.GET("/api/v1/titles/{id}", { params: { path: { id: params.id } } }),
		),
		need(api.GET("/api/v1/admin/providers")),
		need(api.GET("/api/v1/admin/locales")),
	]);
	// A show TMDB has is numbered as one of its episode groups too, where it has
	// any.
	const groups =
		title.kind === "show" && title.ids?.tmdb
			? (
					await api.GET("/api/v1/admin/titles/{id}/episode-groups", {
						params: { path: { id: params.id } },
					})
				).data?.items
			: undefined;
	return { title, providers: providers.items, locales, groups: groups ?? [] };
};
