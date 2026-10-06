import { client, need } from "#lib/api/client.js";
import type { PageLoad } from "./$types";

// The player asks the browser what it plays, so it runs there alone.
export const ssr = false;

// What the player needs before it asks to play: the copies, their tracks,
// chapters, markers and thumbnails, and where the reader left off.
export const load: PageLoad = async ({ fetch, params }) => {
	const api = client(fetch);
	return {
		title: await need(
			api.GET("/api/v1/titles/{id}", { params: { path: { id: params.id } } }),
		),
	};
};
