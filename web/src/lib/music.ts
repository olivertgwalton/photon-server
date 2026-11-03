import type { components } from "./api/schema.d.ts";

type Card = components["schemas"]["Card"];

// An album's tracks in the order it plays them, by disc and then by number,
// as the discs they are on. A track no disc names is on the first.
export function discs(tracks: Card[]): { disc: number; tracks: Card[] }[] {
	const sorted = tracks.toSorted(
		(a, b) =>
			(a.disc_number ?? 1) - (b.disc_number ?? 1) ||
			(a.track_number ?? 0) - (b.track_number ?? 0),
	);
	const out: { disc: number; tracks: Card[] }[] = [];
	for (const track of sorted) {
		const disc = track.disc_number ?? 1;
		const last = out.at(-1);
		if (last?.disc === disc) last.tracks.push(track);
		else out.push({ disc, tracks: [track] });
	}
	return out;
}

// Names joined as Apple Music joins a track's artists: "A & B", "A, B & C".
export function artistLine(names: string[]): string {
	const last = names.at(-1) ?? "";
	return names.length > 1 ? `${names.slice(0, -1).join(", ")} & ${last}` : last;
}

// Who a track is by, wherever it is listed apart from its album.
export function trackArtists(
	track: Pick<Card, "artists" | "artist">,
): string | undefined {
	return track.artists?.length
		? artistLine(track.artists)
		: track.artist?.title;
}

// The artists a track names, where they are others than its album's.
export function guests(track: Card, albumArtist?: string): string | undefined {
	const names = track.artists ?? [];
	if (!names.length || (names.length === 1 && names[0] === albumArtist))
		return undefined;
	return artistLine(names);
}
