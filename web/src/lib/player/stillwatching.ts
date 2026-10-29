// When to ask whether anybody is still there, as Netflix asks a browser and
// as the Photon apps do: after three episodes in a row with nothing pressed,
// counting the one the reader started, and a couple of minutes into the third
// rather than at its start, so a reader who is there loses only a pause.
export const stillWatching = { episodes: 3, leadSeconds: 120 };

// How the next episode came to play: the reader chose it, or the player went
// on by itself.
export type Advance = "reader" | "itself";

// The episodes in a row the next one makes.
export function runAfter(episodes: number, by: Advance): number {
	return by === "itself" ? episodes + 1 : 1;
}

export function isDue(episodes: number, intoSeconds: number): boolean {
	return (
		episodes >= stillWatching.episodes &&
		intoSeconds >= stillWatching.leadSeconds
	);
}
