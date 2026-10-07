<script lang="ts">
import BookmarkIcon from "@lucide/svelte/icons/bookmark";
import FilmIcon from "@lucide/svelte/icons/film";
import GripVerticalIcon from "@lucide/svelte/icons/grip-vertical";
import DownloadIcon from "@lucide/svelte/icons/download";
import HeartIcon from "@lucide/svelte/icons/heart";
import RotateCcwClockIcon from "@lucide/svelte/icons/rotate-ccw-clock";
import HouseIcon from "@lucide/svelte/icons/house";
import ListVideoIcon from "@lucide/svelte/icons/list-video";
import SettingsIcon from "@lucide/svelte/icons/settings";
import TvIcon from "@lucide/svelte/icons/tv";
import type { Component } from "svelte";
import { page } from "$app/state";
import { setLibraryOrder } from "#lib/actions.svelte.js";
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
// While on, each library is dragged into place by its grip, or moved by the
// arrow keys on it, rather than opened.
let reordering = $state(false);
let dragged = $state<string>();
// The order as it is dragged, drawn until the kept one comes back.
let draft = $state<string[]>();
const listed = $derived(
	draft
		? draft.flatMap((id) => libraries.filter((l) => l.id === id))
		: libraries,
);

// The rest of a drag is followed on the window: moving the dragged row in the
// page would take the pointer away from its grip.
function grab(event: PointerEvent, id: string) {
	const list = (event.currentTarget as HTMLElement).closest("ul");
	if (!list) return;
	event.preventDefault();
	dragged = id;
	draft = libraries.map((l) => l.id);
	const drag = (move: PointerEvent) => {
		if (!draft) return;
		// Its place is after every other library whose middle the pointer is past.
		const to = [...list.querySelectorAll<HTMLElement>("[data-library]")].filter(
			(row) => {
				const box = row.getBoundingClientRect();
				return (
					row.dataset.library !== id && move.clientY > box.top + box.height / 2
				);
			},
		).length;
		const ids = draft.filter((other) => other !== id);
		ids.splice(to, 0, id);
		draft = ids;
	};
	const drop = async () => {
		window.removeEventListener("pointermove", drag);
		window.removeEventListener("pointerup", drop);
		window.removeEventListener("pointercancel", drop);
		const ids = draft;
		dragged = undefined;
		if (ids && ids.join() !== libraries.map((l) => l.id).join())
			await setLibraryOrder(ids);
		draft = undefined;
	};
	window.addEventListener("pointermove", drag);
	window.addEventListener("pointerup", drop);
	window.addEventListener("pointercancel", drop);
}

function nudge(event: KeyboardEvent, index: number) {
	const by = { ArrowUp: -1, ArrowDown: 1 }[event.key];
	const to = index + (by ?? 0);
	if (!by || to < 0 || to >= listed.length) return;
	event.preventDefault();
	// From the order drawn, which a move not yet kept has changed.
	const ids = listed.map((l) => l.id);
	[ids[index], ids[to]] = [ids[to], ids[index]];
	draft = ids;
	setLibraryOrder(ids).then(() => {
		if (draft === ids) draft = undefined;
	});
}

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
	index = 0,
)}
	<Sidebar.MenuItem
		class={[
			"group/library",
			library && dragged === library.id && "bg-raise rounded-lg shadow-lg",
		]}
		data-library={library?.id}
	>
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
		{#if library && reordering}
			<button
				type="button"
				class="text-ink-2 hover:bg-raise hover:text-ink focus-visible:outline-signal absolute top-1.5 right-1 grid size-6 cursor-grab touch-none place-items-center rounded-md active:cursor-grabbing group-data-[collapsible=icon]:hidden"
				aria-label="Drag {library.name} into place"
				title="Drag, or use the up and down arrow keys"
				onpointerdown={(event) => grab(event, library.id)}
				onkeydown={(event) => nudge(event, index)}
			>
				<GripVerticalIcon class="size-4" />
			</button>
		{:else if library}
			<LibraryMenu {library} onreorder={() => (reordering = true)} />
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
			<Mark class="shrink-0" />
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
					{#if reordering}
						<button
							type="button"
							class="text-signal hover:text-ink focus-visible:outline-signal absolute top-3.5 right-3 text-xs font-semibold group-data-[collapsible=icon]:hidden"
							onclick={() => (reordering = false)}
						>
							Done
						</button>
					{/if}
					<Sidebar.Menu>
						{#each listed as library, index (library.id)}
							{@render item(
								`/libraries/${library.id}`,
								library.name,
								kindIcons[library.kind],
								library,
								index,
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
					{@render item("/history", "History", RotateCcwClockIcon)}
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
