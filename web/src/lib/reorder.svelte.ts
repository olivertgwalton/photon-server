import type { Attachment } from "svelte/attachments";
import { on } from "svelte/events";

// A list put in order by dragging its rows: each row attaches row(i), and is
// dropped where the row it lands on was.
export function reorder(move: (from: number, to: number) => void) {
	let dragging = $state<number>();
	const row =
		(i: number): Attachment<HTMLElement> =>
		(el) => {
			el.draggable = true;
			const off = [
				on(el, "dragstart", (e) => {
					dragging = i;
					// Firefox drags nothing that carries no data.
					e.dataTransfer?.setData("text/plain", String(i));
				}),
				on(el, "dragover", (e) => e.preventDefault()),
				on(el, "drop", (e) => {
					e.preventDefault();
					if (dragging !== undefined) move(dragging, i);
					dragging = undefined;
				}),
				on(el, "dragend", () => {
					dragging = undefined;
				}),
			];
			return () => {
				for (const stop of off) stop();
			};
		};
	return {
		get dragging() {
			return dragging;
		},
		row,
	};
}
