import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params }) => {
	const api = client(fetch);
	const [title, providers] = await Promise.all([
		need(
			api.GET("/api/v1/titles/{id}", { params: { path: { id: params.id } } }),
		),
		need(api.GET("/api/v1/admin/providers")),
	]);
	return { title, providers: providers.items };
};
