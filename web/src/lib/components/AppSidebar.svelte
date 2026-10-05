<script lang="ts">
import FilmIcon from "@lucide/svelte/icons/film";
import DownloadIcon from "@lucide/svelte/icons/download";
import GaugeIcon from "@lucide/svelte/icons/gauge";
import HeartIcon from "@lucide/svelte/icons/heart";
import HistoryIcon from "@lucide/svelte/icons/history";
import HouseIcon from "@lucide/svelte/icons/house";
import ListVideoIcon from "@lucide/svelte/icons/list-video";
import TvIcon from "@lucide/svelte/icons/tv";
import type { Component } from "svelte";
import { page } from "$app/state";
import type { components } from "#lib/api/schema.js";
import * as Sidebar from "#lib/components/ui/sidebar/index.js";

type Library = components["schemas"]["Library"];

let { libraries, admin }: { libraries: Library[]; admin: boolean } = $props();

const kindIcons: Record<Library["kind"], Component> = {
	movies: FilmIcon,
	shows: TvIcon,
};

const sidebar = Sidebar.useSidebar();

function current(href: string) {
	const path = page.url.pathname;
	return href === "/"
		? path === "/"
		: path === href || path.startsWith(`${href}/`);
}
</script>

{#snippet item(
	href: string,
	label: string,
	Icon: Component,
)}
	<Sidebar.MenuItem>
		<Sidebar.MenuButton isActive={current(href)} tooltipContent={label}>
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
	</Sidebar.MenuItem>
{/snippet}

<Sidebar.Root collapsible="icon">
	<Sidebar.Header>
		<a
			href="/"
			class="font-heading text-ink px-2 py-1.5 text-xl font-bold tracking-tight group-data-[collapsible=icon]:hidden"
		>
			photon
		</a>
	</Sidebar.Header>
	<Sidebar.Content>
		<nav aria-label="Main">
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
							)}
						{/each}
					</Sidebar.Menu>
				</Sidebar.Group>
			{/if}
			<Sidebar.Group>
				<Sidebar.Menu>
					{@render item("/favourites", "Favourites", HeartIcon)}
					{@render item("/playlists", "Playlists", ListVideoIcon)}
					{@render item("/history", "History", HistoryIcon)}
					{@render item("/downloads", "Downloads", DownloadIcon)}
					{#if admin}
						{@render item("/admin", "Dashboard", GaugeIcon)}
					{/if}
				</Sidebar.Menu>
			</Sidebar.Group>
		</nav>
	</Sidebar.Content>
	<Sidebar.Rail />
</Sidebar.Root>
