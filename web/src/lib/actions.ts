import { toast } from "svelte-sonner";
import { act } from "./act.js";
import { client } from "./api/client.js";
import { problemMessage } from "./api/problem.js";
import type { components } from "./api/schema.js";

type RefreshMode = components["schemas"]["RefreshMode"];
type TaskKey = components["schemas"]["TaskKey"];

const api = client();

const path = (id: string) => ({ params: { path: { id } } });

type Mark = "watched" | "favourite" | "watchlist";

export function setMark(id: string, mark: Mark, on: boolean) {
	const at = `/api/v1/titles/{id}/${mark}` as const;
	return act(on ? api.PUT(at, path(id)) : api.DELETE(at, path(id)));
}

export function forgetProgress(id: string) {
	return act(
		api.DELETE("/api/v1/titles/{id}/progress", path(id)),
		"Removed from Continue Watching.",
	);
}

export function addToPlaylist(playlist: string, ids: string[], name: string) {
	return act(
		api.POST("/api/v1/playlists/{id}/entries", {
			...path(playlist),
			body: { item_ids: ids },
		}),
		`Added to ${name}.`,
	);
}

export function addToCollection(
	collection: string,
	ids: string[],
	name: string,
) {
	return act(
		api.POST("/api/v1/admin/collections/{id}/members", {
			...path(collection),
			body: { item_ids: ids },
		}),
		`Added to ${name}.`,
	);
}

export async function newCollection(name: string, ids: string[]) {
	const { data, error } = await api.POST("/api/v1/admin/collections", {
		body: { title: name },
	});
	if (!data) {
		toast.error(problemMessage(error));
		return false;
	}
	return addToCollection(data.id, ids, name);
}

export function newPlaylist(name: string, ids: string[]) {
	return act(
		api.POST("/api/v1/playlists", { body: { name, item_ids: ids } }),
		`Added to ${name}.`,
	);
}

export type RefreshOf = "library" | "title";

export function refresh(
	of: RefreshOf,
	id: string,
	name: string,
	mode: RefreshMode,
) {
	const body = { ...path(id), body: { mode } };
	return act(
		of === "library"
			? api.POST("/api/v1/admin/libraries/{id}/refresh", body)
			: api.POST("/api/v1/admin/titles/{id}/refresh", body),
		mode === "all"
			? `Fetching all metadata for ${name} again.`
			: `Fetching missing metadata for ${name}.`,
	);
}

export function analyseTitle(id: string, name: string) {
	return act(
		api.POST("/api/v1/admin/titles/{id}/analysis", path(id)),
		`Analysing ${name}: its files are read again.`,
	);
}

export function unmatchTitle(id: string, name: string) {
	return act(
		api.DELETE("/api/v1/admin/titles/{id}/match", path(id)),
		`${name} was unmatched: it keeps what its files say until its match is fixed.`,
	);
}

// A plugin's page is opened on the plugin's own origin, at an address with a
// code in it that the plugin claims to know who visits.
export async function visitPage(plugin: string, page: string) {
	const { data, error } = await api.POST(
		"/api/v1/plugins/{slug}/pages/{page}/visits",
		{ params: { path: { slug: plugin, page } } },
	);
	if (error) {
		toast.error(problemMessage(error));
		return;
	}
	window.location.assign(data.url);
}

// The profile's libraries in its own order, as its sidebar lists them.
export function setLibraryOrder(ids: string[]) {
	return act(
		api.PUT("/api/v1/profile/library-order", { body: { library_ids: ids } }),
	);
}

// What an admin can do to a library from the sidebar, as Plex's library menu
// offers: read its folders again, ask its providers again, or remove it.
export function scanLibrary(id: string, name: string) {
	return act(
		api.POST("/api/v1/admin/libraries/{id}/scan", path(id)),
		`${name} is being scanned.`,
	);
}

// Runs a scheduled task now, as its Run now does.
export function runTask(key: TaskKey, said: string) {
	return act(
		api.POST("/api/v1/admin/tasks/{key}/run", { params: { path: { key } } }),
		said,
	);
}

export function splitTitle(id: string, name: string) {
	return act(
		api.POST("/api/v1/admin/titles/{id}/split", path(id)),
		`${name} was split apart.`,
	);
}

export function deleteTitle(id: string, name: string) {
	return act(
		api.DELETE("/api/v1/admin/titles/{id}", path(id)),
		`${name} was deleted.`,
	);
}

// Takes a library's versions on no disk out of it, as Plex's Empty Trash does.
export async function forgetMissingVersions(id: string, name: string) {
	const done = await act(
		api.DELETE("/api/v1/admin/libraries/{id}/missing", path(id)),
	);
	if (!done) return;
	const versions = done.data.versions;
	toast.success(
		versions === 0
			? `${name} had no versions on no disk.`
			: `${versions === 1 ? "One version" : `${versions} versions`} left ${name}.`,
	);
}

// The library's own pages are gone with it: its wall leads home, its settings
// to the list of libraries.
export function removeLibrary(id: string, name: string, here: string) {
	const to = here.startsWith(`/libraries/${id}`)
		? "/"
		: here.startsWith(`/settings/server/libraries/${id}`)
			? "/settings/server/libraries"
			: undefined;
	return act(
		api.DELETE("/api/v1/admin/libraries/{id}", path(id)),
		`${name} was removed.`,
		to,
	);
}
