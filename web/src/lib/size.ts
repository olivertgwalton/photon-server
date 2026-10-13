import type { Attachment } from "svelte/attachments";

// Tells `resized` of an element's box once it is drawn and each time its size
// changes, until it is gone.
export function onResize<T extends Element>(
	resized: (element: T, box: DOMRectReadOnly) => void,
): Attachment<T> {
	return (element) => {
		const watch = new ResizeObserver((entries) => {
			for (const entry of entries) resized(element, entry.contentRect);
		});
		watch.observe(element);
		return () => watch.disconnect();
	};
}
