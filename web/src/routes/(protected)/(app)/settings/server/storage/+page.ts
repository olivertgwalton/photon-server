import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	const [storage, server] = await Promise.all([
		need(client(fetch).GET("/api/v1/admin/storage")),
		need(client(fetch).GET("/api/v1/admin/server")),
	]);
	return { storage, server };
};
