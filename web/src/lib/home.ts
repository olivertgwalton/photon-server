import { client, need } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { keys } from "#lib/changes.js";
import { wallPageSize } from "#lib/wall.js";

// A home row's first page, for the page that draws all of it.
export async function loadRow(
	fetch: typeof globalThis.fetch,
	depends: (...deps: `${string}:${string}`[]) => void,
	kind: components["schemas"]["HomeRowKind"],
) {
	depends(keys.home, keys.userdata);
	const page = await need(
		client(fetch).GET("/api/v1/home/{row}", {
			params: { path: { row: kind }, query: { limit: wallPageSize } },
		}),
	);
	return { kind, page };
}
