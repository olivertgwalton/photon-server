import { client, need } from "#lib/api/client.js";
import type { LayoutLoad } from "./$types";

// Every page under (protected) needs a session; this is the one place that
// says so, by asking who it is: `need` sends a reader without one to log in.
// It only decides what is drawn; the API decides what anyone may do.
// It fetches the vocabulary too, the names every page under it shows values by.
export const load: LayoutLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [me, words] = await Promise.all([
		need(api.GET("/api/v1/profile")),
		need(api.GET("/api/v1/words")),
	]);
	return { me, words };
};
