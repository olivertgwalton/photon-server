import type { client } from "#lib/api/client.js";

// A season's episodes, as cards.
export async function episodesOf(
	api: ReturnType<typeof client>,
	season: string,
) {
	const { data } = await api.GET("/api/v1/titles/{id}", {
		params: { path: { id: season } },
	});
	return data?.episodes?.map((e) => ({ ...e, kind: "episode" as const }));
}
