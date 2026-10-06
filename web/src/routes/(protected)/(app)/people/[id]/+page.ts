import { client, need } from "#lib/api/client.js";
import { keys } from "#lib/changes.js";
import type { PageLoad } from "./$types";

export const load: PageLoad = async ({ fetch, params, depends }) => {
	depends(keys.userdata);
	return {
		person: await need(
			client(fetch).GET("/api/v1/people/{id}", {
				params: { path: { id: params.id } },
			}),
		),
	};
};
