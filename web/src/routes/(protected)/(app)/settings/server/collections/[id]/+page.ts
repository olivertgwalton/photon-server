import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params }) => {
	const api = client(fetch);
	const path = { params: { path: { id: params.id } } };
	const [collection, members] = await Promise.all([
		need(api.GET("/api/v1/titles/{id}", path)),
		need(api.GET("/api/v1/titles/{id}/members", path)),
	]);
	return {
		collection,
		members: members.items,
		library: collection.library_id,
	};
};
