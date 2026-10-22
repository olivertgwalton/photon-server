import { createContext } from "svelte";
import { toast } from "svelte-sonner";
import { goto } from "$app/navigation";
import {
	deleteTitle,
	forgetMissingVersions,
	type RefreshOf,
	removeLibrary,
	splitTitle,
} from "./actions.js";
import { client } from "./api/client.js";
import { problemMessage } from "./api/problem.js";
import type { components } from "./api/schema.js";
import { playHref } from "./format.js";

export type MenuCard = Pick<
	components["schemas"]["Card"],
	"id" | "kind" | "title" | "state" | "show" | "season" | "version_count"
>;
export type EditorTab = "details" | "match" | "artwork";
type VersionUse = "play" | "download";
type Version = components["schemas"]["VersionPage"];

// What the shell draws once and any page under it opens: the title menu and
// the dialogs a title's or a library's menu leads to. Its methods are arrows,
// so a component can hand one on as a handler.
export class Shell {
	// Drawn beside the card's button it was opened from: a menu of its own on
	// every card cost more to draw than the card.
	titleMenu = $state<{ open: boolean; card?: MenuCard; anchor?: HTMLElement }>({
		open: false,
	});

	openTitleMenu = (card: MenuCard, anchor: HTMLElement) => {
		Object.assign(this.titleMenu, { open: true, card, anchor });
	};

	picker = $state({ open: false, ids: [] as string[], title: "" });

	pickPlaylist = (ids: string[], title: string) => {
		Object.assign(this.picker, { open: true, ids, title });
	};

	// For an admin, as Plex's Add to > Collection is.
	collectionPicker = $state({ open: false, id: "", title: "" });

	pickCollection = (id: string, title: string) => {
		Object.assign(this.collectionPicker, { open: true, id, title });
	};

	// Opened at the part asked for, as Plex's Edit and Fix Match are.
	editor = $state({ open: false, id: "", tab: "details" as EditorTab });

	editTitle = (id: string, tab: EditorTab) => {
		Object.assign(this.editor, { open: true, id, tab });
	};

	// How much to refresh, as Jellyfin's Refresh metadata asks.
	refreshing = $state({
		open: false,
		of: "title" as RefreshOf,
		id: "",
		name: "",
	});

	refreshMetadata = (of: RefreshOf, id: string, name: string) => {
		Object.assign(this.refreshing, { open: true, of, id, name });
	};

	// A title's address to pass on, as Plex's Share is: to the browser's own
	// share sheet, else copied. Both want a secure page, which a server on the
	// LAN by its address is not; there the link is shown, chosen, to copy by hand.
	sharing = $state({ open: false, url: "", title: "" });

	shareTitle = async (id: string, title: string) => {
		const url = new URL(`/titles/${id}`, location.origin).href;
		if (navigator.share) {
			try {
				await navigator.share({ title, url });
			} catch (err) {
				if ((err as DOMException).name !== "AbortError") {
					Object.assign(this.sharing, { open: true, url, title });
				}
			}
			return;
		}
		if (navigator.clipboard) {
			await navigator.clipboard.writeText(url);
			toast.success(`The link to ${title} was copied.`);
			return;
		}
		Object.assign(this.sharing, { open: true, url, title });
	};

	// Which version, as Plex's Play Version and Save File ask, of its versions on
	// disk, asked each time. One alone is no choice: it plays, or its download
	// is asked about, at once.
	versions = $state({
		open: false,
		downloading: false,
		id: "",
		title: "",
		use: "play" as VersionUse,
	});
	onDisk = $state.raw<Version[]>([]);
	toDownload = $state.raw<Version>();

	chooseVersion = async (id: string, title: string, use: VersionUse) => {
		const { data, error } = await client().GET("/api/v1/titles/{id}", {
			params: { path: { id } },
		});
		if (!data) {
			toast.error(problemMessage(error));
			return;
		}
		this.onDisk = (data.versions ?? []).filter((v) => !v.missing_since);
		Object.assign(this.versions, { id, title, use });
		if (this.onDisk.length === 1) this.pickVersion(this.onDisk[0]);
		else this.versions.open = true;
	};

	pickVersion = (v: Version) => {
		this.versions.open = false;
		if (this.versions.use === "play") {
			goto(playHref(this.versions.id, { version: v.id }));
			return;
		}
		this.toDownload = v;
		this.versions.downloading = true;
	};

	// As Plex's Search for subtitles is. It opens in the reader's subtitle
	// language; `language` is the one chosen since.
	subtitleSearch = $state({ open: false, id: "", title: "", language: "" });

	findSubtitles = (id: string, title: string) => {
		Object.assign(this.subtitleSearch, { open: true, id, title, language: "" });
	};

	// The question before something that cannot be taken back: what it is,
	// what follows, and the word that does it.
	confirming = $state({
		open: false,
		title: "",
		body: "",
		act: "",
		run: (): unknown => undefined,
	});

	confirmFirst = (
		title: string,
		body: string,
		act: string,
		run: () => unknown,
	) => {
		Object.assign(this.confirming, { open: true, title, body, act, run });
	};

	askToSplit = (id: string, name: string) =>
		this.confirmFirst(
			`Split ${name} apart?`,
			"Each of its versions but the one that plays first becomes a film of its own, matched afresh, and stays so through every scan.",
			"Split apart",
			() => splitTitle(id, name),
		);

	askToDelete = (id: string, name: string) =>
		this.confirmFirst(
			`Delete ${name}?`,
			"Its files are deleted from the disk, a show's or season's episodes with it, and it leaves the library. They cannot be brought back.",
			"Delete",
			() => deleteTitle(id, name),
		);

	askToForgetMissingVersions = (id: string, name: string) =>
		this.confirmFirst(
			`Forget the versions on no disk in ${name}?`,
			"Each version whose files have gone leaves the library, and the titles left with nothing go with them. What is watched of them is forgotten. The files that are still there are not touched.",
			"Forget them",
			() => forgetMissingVersions(id, name),
		);

	askToRemoveLibrary = (id: string, name: string, here: string) =>
		this.confirmFirst(
			`Remove ${name}?`,
			"Its titles, and what everyone has watched of them, are forgotten. The files on disk are not touched.",
			"Remove library",
			() => removeLibrary(id, name, here),
		);
}

export const [getShell, setShell] = createContext<Shell>();
