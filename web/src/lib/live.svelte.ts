import { invalidate, invalidateAll } from "$app/navigation";
import type { components } from "./api/schema.js";
import { affected, type Change, changeKinds } from "./changes.js";

export type Scan = components["schemas"]["Scan"];

// A scan sends a burst of changes; the pages reload once for the burst.
const settle = 1000;

class Live {
	// The libraries being scanned now, by id.
	scans = $state<Record<string, Scan>>({});

	// Listens until the returned function is called. The feed keeps nothing to
	// resend, so after a reconnect every page loads again.
	connect(): () => void {
		const source = new EventSource("/api/v1/events");
		let reconnected = false;
		source.addEventListener("hello", (event: MessageEvent<string>) => {
			const { scans } = JSON.parse(event.data) as { scans: Scan[] };
			this.scans = Object.fromEntries(scans.map((s) => [s.library_id, s]));
			if (reconnected) invalidateAll();
			reconnected = true;
		});
		const stale = new Set<string>();
		let timer: ReturnType<typeof setTimeout> | undefined;
		const take = (event: MessageEvent<string>) => {
			const change = { ...JSON.parse(event.data), kind: event.type } as Change;
			const lib = change.library_id;
			if (change.kind === "scan.progress" && lib) {
				this.scans[lib] = {
					library_id: lib,
					phase: change.details?.phase ?? "reading",
					done: change.details?.done ?? 0,
					known: change.details?.known ?? 0,
					folder: change.details?.folder,
				};
			}
			if (change.kind === "library.scanned" && lib) delete this.scans[lib];
			for (const key of affected(change)) stale.add(key);
			if (stale.size && !timer) {
				timer = setTimeout(() => {
					timer = undefined;
					const keys = [...stale];
					stale.clear();
					for (const key of keys) invalidate(key);
				}, settle);
			}
		};
		for (const kind of changeKinds) source.addEventListener(kind, take);
		return () => {
			clearTimeout(timer);
			source.close();
		};
	}
}

export const live = new Live();
