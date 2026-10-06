import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends("photon:downloads");
	const api = client(fetch);
	const { items } = await need(api.GET("/api/v1/downloads"));
	// A download names its title by id; the page names it by its title.
	const ids = [...new Set(items.map((d) => d.title_id))];
	const titles = await Promise.all(
		ids.map((id) =>
			api
				.GET("/api/v1/titles/{id}", { params: { path: { id } } })
				.then(({ data }) => data),
		),
	);
	const names = new Map(
		titles
			.filter((t) => !!t)
			.map((t) => [t.id, t.show ? `${t.show.title}: ${t.title}` : t.title]),
	);
	return {
		downloads: items.map((d) => ({ ...d, name: names.get(d.title_id) })),
	};
};
