import type { Attachment } from "svelte/attachments";

// A picture the browser has still to fetch fades in once it has; one it holds
// already draws at once, so a wall scrolled back over does not flicker. It is
// marked `data-loading` until then, for its class to fade on.
export const fadeIn: Attachment<HTMLImageElement> = (img) => {
	if (img.complete) return;
	img.dataset.loading = "";
	const done = () => delete img.dataset.loading;
	img.addEventListener("load", done, { once: true });
	img.addEventListener("error", done, { once: true });
	return () => {
		img.removeEventListener("load", done);
		img.removeEventListener("error", done);
	};
};
