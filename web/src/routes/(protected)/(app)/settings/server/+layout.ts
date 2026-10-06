import { client, need } from "#lib/api/client.js";
import type { LayoutLoad } from "./$types";

// The server as an admin sees it. The API refuses anyone else, so a member who
// comes here is shown that refusal rather than the dashboard.
export const load: LayoutLoad = async ({ fetch }) => ({
	server: await need(client(fetch).GET("/api/v1/admin/server")),
});
