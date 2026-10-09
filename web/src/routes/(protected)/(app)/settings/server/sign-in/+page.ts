import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [providers, network] = await Promise.all([
		need(api.GET("/api/v1/admin/sign-in-providers")),
		need(api.GET("/api/v1/admin/network")),
	]);
	return { providers: providers.items, publicURL: network.public_url };
};
