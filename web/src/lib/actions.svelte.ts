import { toast } from "svelte-sonner";
import { refreshAll } from "$app/navigation";
import { page } from "$app/state";
import { act } from "./admin/act.js";
import { client } from "./api/client.js";
import { problemMessage } from "./api/problem.js";
import type { components } from "./api/schema.js";

type RefreshMode = components["schemas"]["RefreshMode"];

type Answer = Promise<{ error?: unknown }>;

// A reader's change, from wherever it is offered: a refusal is told in a
// toast, and what worked redraws the page.
export async function change(call: Answer, said?: string): Promise<boolean> {
	const { error } = await call;
	if (error) {
		toast.error(problemMessage(error));
		return false;
	}
	if (said) toast.success(said);
	await refreshAll();
	return true;
}

const api = client();

const path = (id: string) => ({ params: { path: { id } } });

export function setWatched(id: string, watched: boolean) {
	return change(
		watched
			? api.PUT("/api/v1/titles/{id}/watched", path(id))
			: api.DELETE("/api/v1/titles/{id}/watched", path(id)),
	);
}

export function setFavourite(id: string, favourite: boolean) {
	return change(
		favourite
			? api.PUT("/api/v1/titles/{id}/favourite", path(id))
			: api.DELETE("/api/v1/titles/{id}/favourite", path(id)),
	);
}

export function setWatchlisted(id: string, listed: boolean) {
	return change(
		listed
			? api.PUT("/api/v1/titles/{id}/watchlist", path(id))
			: api.DELETE("/api/v1/titles/{id}/watchlist", path(id)),
	);
}

export function forgetProgress(id: string) {
	return change(
		api.DELETE("/api/v1/titles/{id}/progress", path(id)),
		"Removed from Continue Watching.",
	);
}

export function addToPlaylist(playlist: string, ids: string[], name: string) {
	return change(
		api.POST("/api/v1/playlists/{id}/entries", {
			...path(playlist),
			body: { item_ids: ids },
		}),
		`Added to ${name}.`,
	);
}

export function newPlaylist(name: string, ids: string[]) {
	return change(
		api.POST("/api/v1/playlists", { body: { name, item_ids: ids } }),
		`Added to ${name}.`,
	);
}

// The one "Add to playlist" dialog, drawn by the shell and opened from any
// title's menu.
export const picker = $state({ open: false, ids: [] as string[], title: "" });

export function pickPlaylist(ids: string[], title: string) {
	Object.assign(picker, { open: true, ids, title });
}

export type EditorTab = "details" | "match" | "artwork";

// The one "Edit" dialog, drawn by the shell for an admin and opened from any
// title's menu at the part asked for, as Plex's Edit and Fix Match are.
export const editor = $state({
	open: false,
	id: "",
	tab: "details" as EditorTab,
});

export function editTitle(id: string, tab: EditorTab) {
	Object.assign(editor, { open: true, id, tab });
}

export function refreshTitle(id: string, name: string) {
	return change(
		api.POST("/api/v1/admin/titles/{id}/refresh", {
			...path(id),
			body: { mode: "all" },
		}),
		`Asking the providers about ${name} again.`,
	);
}

// A title's address to pass on, as Plex's Share is: to the browser's own share
// sheet, else copied. Both want a secure page, which a server on the LAN by its
// address is not; there the link is shown, chosen, to copy by hand.
export const sharing = $state({ open: false, url: "", title: "" });

export async function shareTitle(id: string, title: string) {
	const url = new URL(`/titles/${id}`, location.origin).href;
	if (navigator.share) {
		try {
			await navigator.share({ title, url });
		} catch (err) {
			if ((err as DOMException).name !== "AbortError") {
				Object.assign(sharing, { open: true, url, title });
			}
		}
		return;
	}
	if (navigator.clipboard) {
		await navigator.clipboard.writeText(url);
		toast.success(`The link to ${title} was copied.`);
		return;
	}
	Object.assign(sharing, { open: true, url, title });
}

export type VersionUse = "play" | "download";

// The one choice of version, drawn by the shell and opened from any title's
// menu, as Plex's Play Version and Save File ask which copy.
export const versions = $state({
	open: false,
	id: "",
	title: "",
	use: "play" as VersionUse,
});

export function chooseVersion(id: string, title: string, use: VersionUse) {
	Object.assign(versions, { open: true, id, title, use });
}

export function analyseTitle(id: string, name: string) {
	return change(
		api.POST("/api/v1/admin/titles/{id}/analysis", path(id)),
		`Analysing ${name}: its files are read again.`,
	);
}

export function unmatchTitle(id: string, name: string) {
	return change(
		api.DELETE("/api/v1/admin/titles/{id}/match", path(id)),
		`${name} was unmatched: it keeps what its files say until its match is fixed.`,
	);
}

// The one question before something an admin cannot take back from a menu,
// drawn by the shell: what it is, what follows, and the word that does it.
export const confirming = $state({
	open: false,
	title: "",
	body: "",
	act: "",
	run: (): unknown => undefined,
});

export function confirmFirst(
	title: string,
	body: string,
	act: string,
	run: () => unknown,
) {
	Object.assign(confirming, { open: true, title, body, act, run });
}

export function splitTitle(id: string, name: string) {
	confirmFirst(
		`Split ${name} apart?`,
		"Each of its copies but the one that plays first becomes a film of its own, matched afresh, and stays so through every scan.",
		"Split apart",
		() =>
			change(
				api.POST("/api/v1/admin/titles/{id}/split", path(id)),
				`${name} was split apart.`,
			),
	);
}

export function deleteTitle(id: string, name: string) {
	confirmFirst(
		`Delete ${name}?`,
		"Its files are deleted from the disk, a show's or season's episodes with it, and it leaves the library. They cannot be brought back.",
		"Delete",
		() =>
			change(
				api.DELETE("/api/v1/admin/titles/{id}", path(id)),
				`${name} was deleted.`,
			),
	);
}

// What an admin can do to a library from the sidebar, as Plex's library menu
// offers: read its folders again, ask its providers again, or remove it.
export function scanLibrary(id: string, name: string) {
	return change(
		api.POST("/api/v1/admin/libraries/{id}/scan", path(id)),
		`${name} is being scanned.`,
	);
}

export function refreshLibrary(id: string, name: string, mode: RefreshMode) {
	return change(
		api.POST("/api/v1/admin/libraries/{id}/refresh", {
			...path(id),
			body: { mode },
		}),
		mode === "all"
			? `${name} is being described again from its providers.`
			: `${name} is being filled in where it's missing.`,
	);
}

export function removeLibrary(id: string, name: string) {
	confirmFirst(
		`Remove ${name}?`,
		"Its titles, and what everyone has watched of them, are forgotten. The files on disk are not touched.",
		"Remove library",
		// The library's own pages are gone with it.
		() =>
			act(
				api.DELETE("/api/v1/admin/libraries/{id}", path(id)),
				`${name} was removed.`,
				page.url.pathname.includes(`/libraries/${id}`) ? "/" : undefined,
			),
	);
}
