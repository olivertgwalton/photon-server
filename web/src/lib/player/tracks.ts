import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.ts";
import { problemMessage } from "#lib/api/problem.ts";
import { type Listen, trackListen } from "./listen.ts";

// A track, an album or an artist as the tracks it plays, in order: the
// server's own order of what a title plays.
export async function tracksOf(id: string): Promise<Listen[]> {
	const { data, error } = await client().GET("/api/v1/titles/{id}/playable", {
		params: { path: { id } },
	});
	if (!data) {
		toast.error(problemMessage(error));
		return [];
	}
	return data.items.map(trackListen);
}
