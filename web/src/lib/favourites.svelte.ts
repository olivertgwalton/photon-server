import { SvelteMap } from "svelte/reactivity";
import { setMark } from "./actions.ts";

// Titles favourited or unfavourited while the server is asked: shown so at
// once in every menu, until the page is redrawn with its answer or the change
// is refused.
const asked = new SvelteMap<string, boolean>();

export function isFavourite(id: string, at?: string | null): boolean {
	return asked.get(id) ?? !!at;
}

// `saved` keeps what was asked where the page's redraw does not, before it is
// no longer shown as asked.
export async function setFavourite(
	id: string,
	on: boolean,
	saved?: () => void,
): Promise<void> {
	asked.set(id, on);
	try {
		if (await setMark(id, "favourite", on)) saved?.();
	} finally {
		asked.delete(id);
	}
}
