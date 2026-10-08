import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [server, locales] = await Promise.all([
		need(api.GET("/api/v1/admin/server")),
		need(api.GET("/api/v1/admin/locales")),
	]);
	return { server, locales };
};
