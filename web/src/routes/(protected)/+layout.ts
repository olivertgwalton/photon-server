import { client, need } from "#lib/api/client.ts";
import type { LayoutLoad } from "./$types";

// Every page under (protected) needs a session; this is the one place that
// says so, by asking who it is: `need` sends a reader without one to log in.
// It only decides what is drawn; the API decides what anyone may do.
// It fetches the vocabulary too, the names every page under it shows values by,
// and whether the profile hides episode spoilers, which any card may draw.
export const load: LayoutLoad = async ({ fetch }) => {
	const api = client(fetch);
	const [me, words, prefs] = await Promise.all([
		need(api.GET("/api/v1/profile")),
		need(api.GET("/api/v1/words")),
		need(api.GET("/api/v1/profile/preferences")),
	]);
	return { me, words, spoilers: prefs.episode_spoilers };
};
