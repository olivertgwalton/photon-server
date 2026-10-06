import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, depends }) => {
	depends("photon:playlists");
	return {
		playlists: (await need(client(fetch).GET("/api/v1/playlists"))).items,
	};
};
