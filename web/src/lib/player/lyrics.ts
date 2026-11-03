import { client } from "#lib/api/client.ts";
import type { components } from "#lib/api/schema.d.ts";

type Lyrics = components["schemas"]["Lyrics"];

// The lyrics of the last few versions asked for, so Now Playing's lyrics and
// its menu's Copy lyrics ask once between them.
const asked = new Map<string, Promise<Lyrics | null>>();
const kept = 8;

// A track's lyrics, as the version playing has them: null for none.
export function lyricsOf(id: string, version?: string): Promise<Lyrics | null> {
	const key = `${id}/${version ?? ""}`;
	const known = asked.get(key);
	if (known) return known;
	const found = load(id, version);
	asked.set(key, found);
	// None, or a request that failed, is asked again next time: what the
	// server fetches meanwhile may have some.
	found.then(
		(l) => l ?? asked.delete(key),
		() => asked.delete(key),
	);
	for (const old of asked.keys()) {
		if (asked.size <= kept) break;
		asked.delete(old);
	}
	return found;
}

async function load(id: string, version?: string): Promise<Lyrics | null> {
	const { data, response } = await client().GET("/api/v1/titles/{id}/lyrics", {
		params: { path: { id }, query: { version_id: version } },
	});
	if (response.status === 404) return null;
	if (!data) throw new Error(`lyrics: ${response.status}`);
	return data;
}
