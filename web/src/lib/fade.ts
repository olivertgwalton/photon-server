import type { Attachment } from "svelte/attachments";
import { on } from "svelte/events";

// A picture the browser has still to fetch fades in once it has; one it holds
// already draws at once, so a wall scrolled back over does not flicker. It is
// marked `data-loading` until then, for its class to fade on.
export const fadeIn: Attachment<HTMLImageElement> = (img) => {
	if (img.complete) return;
	img.dataset.loading = "";
	const done = () => delete img.dataset.loading;
	const off = [
		on(img, "load", done, { once: true }),
		on(img, "error", done, { once: true }),
	];
	return () => {
		for (const stop of off) stop();
	};
};
