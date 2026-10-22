import type { components } from "#lib/api/schema.js";
import { page } from "$app/state";

type Schemas = components["schemas"];

// Whether an episode the profile has not begun is drawn by its blurhash in
// place of its picture, by its episode_spoilers, which every page under (protected) loads.
// One part way through, or watched, is not.
export function hidesSpoiler(card: Pick<Schemas["Card"], "kind" | "state">) {
	return (
		(page.data.spoilers as Schemas["SpoilerMode"] | undefined) === "hide" &&
		card.kind === "episode" &&
		!card.state?.watched_at &&
		!card.state?.position_ms
	);
}
