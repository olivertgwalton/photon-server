import { toast } from "svelte-sonner";
import { refreshAll } from "$app/navigation";
import { client } from "./api/client.js";
import { problemMessage } from "./api/problem.js";

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
