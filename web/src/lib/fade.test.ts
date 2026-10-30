import { expect, test } from "bun:test";
import { fadeIn } from "./fade.ts";

// A card is handed another title's picture as a wall follows the server, and
// the one it is handed may be in the browser's store already: it is there
// before this watches for it, with the mark the last one left still on.
test("a picture already there is not left behind its blur", () => {
	const img = {
		complete: true,
		dataset: { loading: "" },
	} as unknown as HTMLImageElement;

	fadeIn(img);

	expect(img.dataset.loading).toBeUndefined();
});
