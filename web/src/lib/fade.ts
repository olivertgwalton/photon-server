import type { Attachment } from "svelte/attachments";
import { on } from "svelte/events";

// A picture the browser has still to fetch fades in once it has; one it holds
// already draws at once, so a wall scrolled back over does not flicker. It is
// marked `data-loading` until then, for its class to fade on. A card handed
// another picture is here again with the mark still on it: one out of the
// browser's own store is there before this watches for it, so the mark comes
// off rather than staying for an arrival that is past.
export const fadeIn: Attachment<HTMLImageElement> = (img) => {
	const done = () => delete img.dataset.loading;
	if (img.complete) {
		done();
		return;
	}
	img.dataset.loading = "";
	const off = [
		on(img, "load", done, { once: true }),
		on(img, "error", done, { once: true }),
	];
	return () => {
		for (const stop of off) stop();
	};
};
