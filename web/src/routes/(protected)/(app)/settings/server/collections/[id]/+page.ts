import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params }) => {
	const api = client(fetch);
	const path = { params: { path: { id: params.id } } };
	const [collection, members, libraries] = await Promise.all([
		need(api.GET("/api/v1/titles/{id}", path)),
		need(api.GET("/api/v1/titles/{id}/members", path)),
		need(api.GET("/api/v1/admin/libraries")),
	]);
	// A collection holds its own library's titles only, and says nothing of
	// which that is, so its library is found among the libraries' collections.
	const shelves = await Promise.all(
		libraries.items
			.filter((library) => library.counts.collections)
			.map(async (library) => {
				const { data } = await api.GET("/api/v1/libraries/{id}/collections", {
					params: { path: { id: library.id }, query: { limit: 200 } },
				});
				return data?.items.some((c) => c.id === params.id)
					? library.id
					: undefined;
			}),
	);
	return {
		collection,
		members: members.items,
		library: shelves.find(Boolean),
	};
};
