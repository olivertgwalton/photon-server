<script lang="ts">
import BookmarkIcon from "@lucide/svelte/icons/bookmark";
import BookmarkXIcon from "@lucide/svelte/icons/bookmark-x";
import CheckIcon from "@lucide/svelte/icons/check";
import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
import EyeOffIcon from "@lucide/svelte/icons/eye-off";
import HeartIcon from "@lucide/svelte/icons/heart";
import HeartOffIcon from "@lucide/svelte/icons/heart-off";
import ImageIcon from "@lucide/svelte/icons/image";
import ListPlusIcon from "@lucide/svelte/icons/list-plus";
import PencilIcon from "@lucide/svelte/icons/pencil";
import PlayIcon from "@lucide/svelte/icons/play";
import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
import RotateCcwIcon from "@lucide/svelte/icons/rotate-ccw";
import SearchCheckIcon from "@lucide/svelte/icons/search-check";
import SettingsIcon from "@lucide/svelte/icons/settings";
import TvIcon from "@lucide/svelte/icons/tv";
import UndoIcon from "@lucide/svelte/icons/undo-2";
import { goto } from "$app/navigation";
import { page } from "$app/state";
import {
	editTitle,
	forgetProgress,
	pickPlaylist,
	refreshTitle,
	setFavourite,
	setWatched,
	setWatchlisted,
} from "#lib/actions.svelte.js";
import type { components } from "#lib/api/schema.js";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.js";
import { playHref } from "#lib/format.js";
import { cn } from "#lib/utils.js";

type Card = components["schemas"]["Card"];

// What can be done to a title without opening it, as the title's page offers.
let {
	card,
	class: className,
}: {
	card: Pick<Card, "id" | "kind" | "title" | "state" | "show">;
	class?: string;
} = $props();

const playable = $derived(
	card.kind === "movie" || card.kind === "episode" || card.kind === "extra",
);
// A show or season is watched once every episode is.
const watched = $derived(!!card.state?.watched_at);
const favourite = $derived(!!card.state?.favourite_at);
const started = $derived(!!card.state?.position_ms);
const watchlisted = $derived(!!card.state?.watchlisted_at);
const admin = $derived(page.data.me?.role === "admin");
// What an admin can do to it, as Plex's and Jellyfin's card menus offer: a
// film or show is matched, and all but a collection or an extra refreshed.
const matched = $derived(card.kind === "movie" || card.kind === "show");
const refreshes = $derived(card.kind !== "collection" && card.kind !== "extra");
const name = $derived(
	card.show ? `${card.show.title}: ${card.title}` : card.title,
);
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger
		class={cn(
			"bg-ground/80 text-ink hover:bg-ink hover:text-ground grid size-8 place-items-center rounded-full backdrop-blur",
			className,
		)}
		aria-label="More for {name}"
	>
		<EllipsisIcon class="size-4" />
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end" class="w-60">
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
		{#if card.kind !== "collection"}
			<DropdownMenu.Item onSelect={() => setWatched(card.id, !watched)}>
				{#if watched}
					<UndoIcon />Mark as unwatched
				{:else}
					<CheckIcon />Mark as watched
				{/if}
			</DropdownMenu.Item>
		{/if}
		<DropdownMenu.Item onSelect={() => setFavourite(card.id, !favourite)}>
			{#if favourite}
				<HeartOffIcon />Remove from favourites
			{:else}
				<HeartIcon />Add to favourites
			{/if}
		</DropdownMenu.Item>
		{#if card.kind !== "collection" && card.kind !== "extra"}
			<DropdownMenu.Item onSelect={() => setWatchlisted(card.id, !watchlisted)}>
				{#if watchlisted}
					<BookmarkXIcon />Remove from watchlist
				{:else}
					<BookmarkIcon />Add to watchlist
				{/if}
			</DropdownMenu.Item>
		{/if}
		{#if card.kind !== "extra"}
			<DropdownMenu.Item onSelect={() => pickPlaylist([card.id], name)}>
				<ListPlusIcon />Add to playlist…
			</DropdownMenu.Item>
		{/if}
		{#if started && playable}
			<DropdownMenu.Item onSelect={() => forgetProgress(card.id)}>
				<EyeOffIcon />Remove from Continue Watching
			</DropdownMenu.Item>
		{/if}
		{#if card.show}
			{@const show = card.show}
			<DropdownMenu.Separator />
			<DropdownMenu.Item onSelect={() => goto(`/titles/${show.id}`)}>
				<TvIcon />Go to {show.title}
			</DropdownMenu.Item>
		{/if}
		{#if admin}
			<DropdownMenu.Separator />
			<DropdownMenu.Item onSelect={() => editTitle(card.id, "details")}>
				<PencilIcon />Edit…
			</DropdownMenu.Item>
			{#if matched}
				<DropdownMenu.Item onSelect={() => editTitle(card.id, "match")}>
					<SearchCheckIcon />Fix match…
				</DropdownMenu.Item>
			{/if}
			{#if card.kind !== "extra"}
				<DropdownMenu.Item onSelect={() => editTitle(card.id, "artwork")}>
					<ImageIcon />Choose artwork…
				</DropdownMenu.Item>
			{/if}
			{#if refreshes}
				<DropdownMenu.Item onSelect={() => refreshTitle(card.id, name)}>
					<RefreshCwIcon />Refresh metadata
				</DropdownMenu.Item>
			{/if}
			<DropdownMenu.Item
				onSelect={() => goto(`/settings/server/titles/${card.id}`)}
			>
				<SettingsIcon />Manage…
			</DropdownMenu.Item>
		{/if}
	</DropdownMenu.Content>
</DropdownMenu.Root>
