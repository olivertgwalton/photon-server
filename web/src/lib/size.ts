import type { Attachment } from "svelte/attachments";

// Tells `resized` of an element's box once it is drawn and each time its size
// changes, until it is gone. The frame after the browser's, not during it: a
// `resized` that lays the element out again — as a wall does, from its own
// width — would otherwise leave the browser's round of observers undelivered,
// which it reports to the console as an error.
export function onResize<T extends Element>(
	resized: (element: T, box: DOMRectReadOnly) => void,
): Attachment<T> {
	return (element) => {
		let frame = 0;
		const watch = new ResizeObserver(([entry]) => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => resized(element, entry.contentRect));
		});
		watch.observe(element);
		return () => {
			cancelAnimationFrame(frame);
			watch.disconnect();
		};
	};
}
