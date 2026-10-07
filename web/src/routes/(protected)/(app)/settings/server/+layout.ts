import { client, need } from "#lib/api/client.js";
import type { LayoutLoad } from "./$types";

// The server as an admin sees it, and its nodes. The API refuses anyone else, so a member who
// comes here is shown that refusal rather than the dashboard.
export const load: LayoutLoad = async ({ fetch }) => {
	const [server, nodes] = await Promise.all([
		need(client(fetch).GET("/api/v1/admin/server")),
		need(client(fetch).GET("/api/v1/admin/nodes")),
	]);
	return { server, nodes: nodes.items };
};
