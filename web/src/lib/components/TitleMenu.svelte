<script lang="ts">
import BookmarkIcon from "@lucide/svelte/icons/bookmark";
import BookmarkXIcon from "@lucide/svelte/icons/bookmark-x";
import CaptionsIcon from "@lucide/svelte/icons/captions";
import CheckIcon from "@lucide/svelte/icons/check";
import DownloadIcon from "@lucide/svelte/icons/download";
import EyeOffIcon from "@lucide/svelte/icons/eye-off";
import HeartIcon from "@lucide/svelte/icons/heart";
import HeartOffIcon from "@lucide/svelte/icons/heart-off";
import ImageIcon from "@lucide/svelte/icons/image";
import LayersIcon from "@lucide/svelte/icons/layers";
import LibraryBigIcon from "@lucide/svelte/icons/library-big";
import ListPlusIcon from "@lucide/svelte/icons/list-plus";
import MergeIcon from "@lucide/svelte/icons/merge";
import PencilIcon from "@lucide/svelte/icons/pencil";
import PlayIcon from "@lucide/svelte/icons/play";
import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
import RotateCcwIcon from "@lucide/svelte/icons/rotate-ccw";
import ScanSearchIcon from "@lucide/svelte/icons/scan-search";
import SearchCheckIcon from "@lucide/svelte/icons/search-check";
import SearchXIcon from "@lucide/svelte/icons/search-x";
import SettingsIcon from "@lucide/svelte/icons/settings";
import Share2Icon from "@lucide/svelte/icons/share-2";
import SplitIcon from "@lucide/svelte/icons/split";
import TrashIcon from "@lucide/svelte/icons/trash";
import TvIcon from "@lucide/svelte/icons/tv";
import UndoIcon from "@lucide/svelte/icons/undo-2";
import {
	analyseTitle,
	forgetProgress,
	setMark,
	unmatchTitle,
} from "#lib/actions.ts";
import { matchable, refreshable } from "#lib/admin/edit.ts";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.ts";
import { playHref, titleWithShow } from "#lib/format.ts";
import { getShell } from "#lib/shell.svelte.ts";
import { goto } from "$app/navigation";
import { page } from "$app/state";

const shell = getShell();

// What can be done to a title without opening it, as the title's page offers.
// It is the card's that last opened it.
const card = $derived(shell.titleMenu.card);

const playable = $derived(
	card?.kind === "movie" || card?.kind === "episode" || card?.kind === "extra",
);
// A show or season is watched once every episode is.
const watched = $derived(!!card?.state?.watched_at);
const favourite = $derived(!!card?.state?.favourite_at);
const started = $derived(!!card?.state?.position_ms);
const watchlisted = $derived(!!card?.state?.watchlisted_at);
const admin = $derived(page.data.me?.role === "admin");
const matched = $derived(card ? matchable(card.kind) : false);
const refreshes = $derived(card ? refreshable(card.kind) : false);
const name = $derived(card ? titleWithShow(card) : "");
</script>

<DropdownMenu.Root bind:open={shell.titleMenu.open}>
	<DropdownMenu.Content
		align="end"
		class="w-60"
		customAnchor={shell.titleMenu.anchor}
		onInteractOutside={(e) => {
			// Its button closes it, as a second click on a menu's trigger does.
			if (shell.titleMenu.anchor?.contains(e.target as Node))
				e.preventDefault();
		}}
		onCloseAutoFocus={(e) => {
			e.preventDefault();
			shell.titleMenu.anchor?.focus();
		}}
	>
		{#if card}
			{#if playable}
				<DropdownMenu.Item onSelect={() => goto(playHref(card.id))}>
					<PlayIcon />{started ? "Resume" : "Play"}
				</DropdownMenu.Item>
			{/if}
			{#if playable && started}
				<DropdownMenu.Item onSelect={() => goto(playHref(card.id, { t: 0 }))}>
					<RotateCcwIcon />Play from the beginning
				</DropdownMenu.Item>
			{/if}
			{#if playable && (card.version_count ?? 0) > 1}
				<DropdownMenu.Item
					onSelect={() => shell.chooseVersion(card.id, name, "play")}
				>
					<LayersIcon />Play version…
				</DropdownMenu.Item>
			{/if}
			{#if card.kind !== "collection"}
				<DropdownMenu.Item
					onSelect={() => setMark(card.id, "watched", !watched)}
				>
					{#if watched}
						<UndoIcon />Mark as unwatched
					{:else}
						<CheckIcon />Mark as watched
					{/if}
				</DropdownMenu.Item>
			{/if}
			<DropdownMenu.Item
				onSelect={() => setMark(card.id, "favourite", !favourite)}
			>
				{#if favourite}
					<HeartOffIcon />Remove from favourites
				{:else}
					<HeartIcon />Add to favourites
				{/if}
			</DropdownMenu.Item>
			{#if card.kind !== "collection" && card.kind !== "extra"}
				<DropdownMenu.Item
					onSelect={() => setMark(card.id, "watchlist", !watchlisted)}
				>
					{#if watchlisted}
						<BookmarkXIcon />Remove from watchlist
					{:else}
						<BookmarkIcon />Add to watchlist
					{/if}
				</DropdownMenu.Item>
			{/if}
			{#if card.kind !== "extra"}
				<DropdownMenu.Item onSelect={() => shell.pickPlaylist([card.id], name)}>
					<ListPlusIcon />Add to playlist…
				</DropdownMenu.Item>
			{/if}
			{#if playable && card.version_count}
				<DropdownMenu.Item
					onSelect={() => shell.chooseVersion(card.id, name, "download")}
				>
					<DownloadIcon />Download…
				</DropdownMenu.Item>
			{/if}
			{#if card.kind === "movie" || card.kind === "episode"}
				<DropdownMenu.Item onSelect={() => shell.findSubtitles(card.id, name)}>
					<CaptionsIcon />Find subtitles…
				</DropdownMenu.Item>
			{/if}
			<DropdownMenu.Item onSelect={() => shell.shareTitle(card.id, name)}>
				<Share2Icon />Share…
			</DropdownMenu.Item>
			{#if started && playable}
				<DropdownMenu.Item onSelect={() => forgetProgress(card.id)}>
					<EyeOffIcon />Remove from Continue Watching
				</DropdownMenu.Item>
			{/if}
			{#if card.show}
				{@const show = card.show}
				<DropdownMenu.Separator />
				{#if card.season}
					{@const season = card.season}
					<DropdownMenu.Item onSelect={() => goto(`/titles/${season.id}`)}>
						<TvIcon />Go to {season.title}
					</DropdownMenu.Item>
				{/if}
				<DropdownMenu.Item onSelect={() => goto(`/titles/${show.id}`)}>
					<TvIcon />Go to {show.title}
				</DropdownMenu.Item>
			{/if}
			{#if admin}
				<DropdownMenu.Separator />
				{#if card.kind !== "collection" && card.kind !== "extra"}
					<DropdownMenu.Item
						onSelect={() => shell.pickCollection(card.id, name)}
					>
						<LibraryBigIcon />Add to collection…
					</DropdownMenu.Item>
				{/if}
				<DropdownMenu.Item onSelect={() => shell.editTitle(card.id, "details")}>
					<PencilIcon />Edit…
				</DropdownMenu.Item>
				{#if matched}
					<DropdownMenu.Item onSelect={() => shell.editTitle(card.id, "match")}>
						<SearchCheckIcon />Fix match…
					</DropdownMenu.Item>
					<DropdownMenu.Item onSelect={() => unmatchTitle(card.id, name)}>
						<SearchXIcon />Unmatch
					</DropdownMenu.Item>
					{#if card.kind === "movie" && (card.version_count ?? 0) > 1}
						<DropdownMenu.Item onSelect={() => shell.askToSplit(card.id, name)}>
							<SplitIcon />Split apart…
						</DropdownMenu.Item>
					{/if}
					{#if card.kind === "movie"}
						<DropdownMenu.Item
							onSelect={() =>
								goto(`/settings/server/titles/${card.id}#versions`)}
						>
							<MergeIcon />Merge…
						</DropdownMenu.Item>
					{/if}
				{/if}
				{#if card.kind !== "extra"}
					<DropdownMenu.Item
						onSelect={() => shell.editTitle(card.id, "artwork")}
					>
						<ImageIcon />Choose artwork…
					</DropdownMenu.Item>
				{/if}
				{#if refreshes}
					<DropdownMenu.Item
						onSelect={() => shell.refreshMetadata("title", card.id, name)}
					>
						<RefreshCwIcon />Refresh metadata…
					</DropdownMenu.Item>
				{/if}
				{#if card.kind !== "collection"}
					<DropdownMenu.Item onSelect={() => analyseTitle(card.id, name)}>
						<ScanSearchIcon />Analyse
					</DropdownMenu.Item>
				{/if}
				<DropdownMenu.Item
					onSelect={() => goto(`/settings/server/titles/${card.id}`)}
				>
					<SettingsIcon />Manage…
				</DropdownMenu.Item>
				{#if card.kind !== "collection"}
					<DropdownMenu.Separator />
					<DropdownMenu.Item
						variant="destructive"
						onSelect={() => shell.askToDelete(card.id, name)}
					>
						<TrashIcon />Delete…
					</DropdownMenu.Item>
				{/if}
			{/if}
		{/if}
	</DropdownMenu.Content>
</DropdownMenu.Root>
