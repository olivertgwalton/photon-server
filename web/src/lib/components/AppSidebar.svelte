<script lang="ts">
import BookmarkIcon from "@lucide/svelte/icons/bookmark";
import FilmIcon from "@lucide/svelte/icons/film";
import DownloadIcon from "@lucide/svelte/icons/download";
import HeartIcon from "@lucide/svelte/icons/heart";
import HistoryIcon from "@lucide/svelte/icons/history";
import HouseIcon from "@lucide/svelte/icons/house";
import ListVideoIcon from "@lucide/svelte/icons/list-video";
import SettingsIcon from "@lucide/svelte/icons/settings";
import TvIcon from "@lucide/svelte/icons/tv";
import type { Component } from "svelte";
import { page } from "$app/state";
import type { components } from "#lib/api/schema.js";
import * as Sidebar from "#lib/components/ui/sidebar/index.js";
import LibraryMenu from "./LibraryMenu.svelte";
import Mark from "./Mark.svelte";

type Library = components["schemas"]["Library"];

let { libraries }: { libraries: Library[] } = $props();

const kindIcons: Record<Library["kind"], Component> = {
	movies: FilmIcon,
	shows: TvIcon,
};

const sidebar = Sidebar.useSidebar();
const admin = $derived(page.data.me?.role === "admin");

// The page's own item is marked by a bar at its edge that grows in.
const menuButton =
	"relative h-9 gap-3 rounded-lg transition-[width,height,padding,color,background-color] duration-200 before:absolute before:inset-y-2 before:left-0 before:w-0.75 before:scale-y-0 before:rounded-full before:bg-sidebar-primary before:transition-transform before:duration-300 data-active:font-semibold data-active:before:scale-y-100";

function current(href: string) {
	const path = page.url.pathname;
	if (href === "/") return path === "/";
	// The server's settings are the dashboard's, not the reader's.
	return path === href || path.startsWith(`${href}/`);
}
</script>

{#snippet item(
	href: string,
	label: string,
	Icon: Component,
	library?: Library,
)}
	<Sidebar.MenuItem class="group/library">
		<Sidebar.MenuButton
			isActive={current(href)}
			tooltipContent={label}
			class={menuButton}
		>
			{#snippet child({
				props,
			})}
				<a
					{href}
					{...props}
					aria-current={current(href) ? "page" : undefined}
					onclick={() => sidebar.setOpenMobile(false)}
				>
					<Icon />
					<span>{label}</span>
				</a>
			{/snippet}
		</Sidebar.MenuButton>
		{#if library && admin}
			<LibraryMenu {library} />
		{/if}
	</Sidebar.MenuItem>
{/snippet}

<Sidebar.Root collapsible="icon">
	<Sidebar.Header>
		<a
			href="/"
			aria-label="photon"
			class="text-ink hover:text-ink-2 flex items-center gap-2.5 px-2 py-1.5 transition-colors duration-200 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0"
		>
			<Mark size={24} class="shrink-0" />
			<span
				class="font-heading text-xl font-black tracking-tighter group-data-[collapsible=icon]:hidden"
			>
				photon.
			</span>
		</a>
	</Sidebar.Header>
	<Sidebar.Content>
		<nav aria-label="Main" class="flex flex-1 flex-col">
			<Sidebar.Group>
				<Sidebar.Menu>{@render item("/", "Home", HouseIcon)}</Sidebar.Menu>
			</Sidebar.Group>
			{#if libraries.length}
				<Sidebar.Group>
					<Sidebar.GroupLabel>Libraries</Sidebar.GroupLabel>
					<Sidebar.Menu>
						{#each libraries as library (library.id)}
							{@render item(
								`/libraries/${library.id}`,
								library.name,
								kindIcons[library.kind],
								library,
							)}
						{/each}
					</Sidebar.Menu>
				</Sidebar.Group>
			{/if}
			<Sidebar.Group>
				<Sidebar.Menu>
					{@render item("/watchlist", "Watchlist", BookmarkIcon)}
					{@render item("/favourites", "Favourites", HeartIcon)}
					{@render item("/playlists", "Playlists", ListVideoIcon)}
					{@render item("/history", "History", HistoryIcon)}
					{@render item("/downloads", "Downloads", DownloadIcon)}
				</Sidebar.Menu>
			</Sidebar.Group>
			<Sidebar.Group class="mt-auto">
				<Sidebar.Menu>
					{@render item("/settings", "Settings", SettingsIcon)}
				</Sidebar.Menu>
			</Sidebar.Group>
		</nav>
	</Sidebar.Content>
	<Sidebar.Rail />
</Sidebar.Root>
