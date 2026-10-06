import { client, need } from "#lib/api/client.js";
import type { LayoutLoad } from "./$types";

// Every page under (protected) needs a session; this is the one place that
// says so, by asking who it is: `need` sends a reader without one to log in.
// It only decides what is drawn; the API decides what anyone may do.
export const load: LayoutLoad = async ({ fetch }) => ({
	me: await need(client(fetch).GET("/api/v1/me")),
});
