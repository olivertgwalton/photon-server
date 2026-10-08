import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	const network = await need(client(fetch).GET("/api/v1/admin/network"));
	return { network };
};
