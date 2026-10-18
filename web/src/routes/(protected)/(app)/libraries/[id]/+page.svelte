<script lang="ts">
import LayoutGridIcon from "@lucide/svelte/icons/layout-grid";
import ListIcon from "@lucide/svelte/icons/list";
import RectangleHorizontalIcon from "@lucide/svelte/icons/rectangle-horizontal";
import { act } from "#lib/act.js";
import { withQuery } from "#lib/address.js";
import { client, refusal } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import LetterBar from "#lib/components/LetterBar.svelte";
import SmartSaveDialog from "#lib/components/SmartSaveDialog.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as ToggleGroup from "#lib/components/ui/toggle-group/index.js";
import Wall from "#lib/components/Wall.svelte";
import WallFilterMenu from "#lib/components/WallFilterMenu.svelte";
import WallSortMenu from "#lib/components/WallSortMenu.svelte";
import { count } from "#lib/format.js";
import { gridColumns } from "#lib/grid.js";
import {
	cleared,
	filterCount,
	letterOffset,
	playlistRule,
	smartRule,
	type ViewStyle,
	viewKey,
	type WallQuery,
	wallPageSize,
	wallSearch,
} from "#lib/wall.js";
import { goto } from "$app/navigation";
import { page } from "$app/state";

type Letter = components["schemas"]["Letter"];

let { data } = $props();

let wall = $state<Wall>();
let view = $derived(data.view);
// The wall, or why the server refused it. Another query's stays, dimmed,
// until this one's comes, and the page then changes at once.
const shown = $derived(
	data.wall.catch((e: unknown) => ({ refused: refusal(e) })),
);
// Empty card frames, a screenful, until the wall arrives.
const blanks = Array.from({ length: 24 }, (_, n) => n);

const id = $derived(data.library.id);
const episodes = $derived(data.query.kind === "episodes");
const facets = $derived(data.kindFacets ?? data.facets);
// What a library's wall may page besides all its titles, as Plex's type picker
// and Jellyfin's tabs offer it.
const kinds = $derived(
	(
		{
			movies: [],
			shows: [
				["titles", "Shows"],
				["episodes", "Episodes"],
			],
			mixed: [
				["titles", "All"],
				["films", "Films"],
				["shows", "Shows"],
				["episodes", "Episodes"],
			],
		} satisfies Record<
			components["schemas"]["LibraryKind"],
			[components["schemas"]["WallKind"], string][]
		>
	)[data.library.kind],
);
const narrowed = $derived(filterCount(data.query) > 0);
const worth = $derived(narrowed || Boolean(data.query.sort));
// What an admin may keep of this wall as a smart collection.
const rule = $derived(
	data.me.role === "admin" && worth ? smartRule(data.query) : undefined,
);
// And what anyone may keep of it as a playlist of their own.
const playRule = $derived(
	worth ? playlistRule(data.query, data.library.kind) : undefined,
);
let saving = $derived.by(() => {
	id;
	return false;
});
let keeping = $derived.by(() => {
	id;
	return false;
});

// The filters live in the address: a narrowed wall can be shared, and Back
// leaves the library rather than undoing a filter.
function show(query: WallQuery) {
	const wall = new URL(`/libraries/${id}${wallSearch(query)}`, page.url.href);
	goto(
		withQuery(wall, { collection: data.editing, playlist: data.editingList }),
		{
			replace: true,
			reset: false,
		},
	);
}

function saveRule() {
	if (!rule || !data.editing) return;
	return act(
		client().PUT("/api/v1/admin/collections/{id}/rule", {
			params: { path: { id: data.editing } },
			body: rule,
		}),
		"Saved. The collection holds what these filters find.",
		`/settings/server/collections/${data.editing}`,
	);
}

function saveListRule() {
	if (!playRule || !data.editingList) return;
	return act(
		client().PUT("/api/v1/playlists/{id}/rule", {
			params: { path: { id: data.editingList } },
			body: playRule,
		}),
		"Saved. The playlist plays what these filters find.",
		`/playlists/${data.editingList}`,
	);
}

// Keep this wall as a new smart collection, everyone's, or a smart playlist,
// the profile's own.
async function keepCollection(title: string) {
	if (!rule) return;
	return act(
		client().POST("/api/v1/admin/collections", {
			body: { library_ids: [id], title, rule },
		}),
		`${title} is a smart collection.`,
		(made) => `/settings/server/collections/${made.id}`,
	);
}

async function keepPlaylist(name: string) {
	if (!playRule) return;
	return act(
		client().POST("/api/v1/playlists", {
			body: { name, library_id: id, rule: playRule },
		}),
		`${name} plays what these filters find.`,
		(made) => `/playlists/${made.id}`,
	);
}

function choose(style: string) {
	if (!style) return;
	view = style as ViewStyle;
	try {
		localStorage.setItem(viewKey(id), style);
	} catch {
		// Not kept where storage is refused; drawn this way until the page goes.
	}
}

async function fetchPage(offset: number) {
	const { data: page } = await client().GET("/api/v1/libraries/{id}/titles", {
		params: {
			path: { id },
			query: { ...data.query, offset, limit: wallPageSize },
		},
	});
	return page?.items;
}

function jump(letters: Letter[], letter: string) {
	wall?.jump(letterOffset(letters, letter, data.query.order ?? "asc"));
}
</script>

<svelte:head><title>{data.library.name} · Photon</title></svelte:head>

<h2 class="sr-only">Titles</h2>
<div class="flex flex-wrap items-center gap-2">
	{#if kinds.length}
		<ToggleGroup.Root
			type="single"
			variant="outline"
			size="sm"
			value={data.query.kind ?? "titles"}
			onValueChange={(kind) =>
				kind &&
				show({
					...data.query,
					kind:
						kind === "titles"
							? undefined
							: (kind as components["schemas"]["WallKind"]),
				})}
			aria-label="Show"
		>
			{#each kinds as [value, label] (value)}
				<ToggleGroup.Item {value}>{label}</ToggleGroup.Item>
			{/each}
		</ToggleGroup.Root>
	{/if}
	<p class="text-ink-3 mr-auto text-sm" aria-live="polite">
		<svelte:boundary>
			{@const answer = await shown}
			{#if "titles" in answer}
				{count(answer.titles.total, episodes ? "episode" : "title")}
			{/if}
			{#snippet pending()}
				<!-- No count until the wall comes. -->
			{/snippet}
		</svelte:boundary>
	</p>
	{#if narrowed}
		<Button variant="ghost" size="sm" onclick={() => show(cleared(data.query))}>
			Clear filters
		</Button>
	{/if}
	{#if rule && data.editing}
		<Button size="sm" onclick={saveRule}>Save to the collection</Button>
	{:else if rule}
		<Button variant="outline" size="sm" onclick={() => (saving = true)}>
			Save as a smart collection
		</Button>
		<SmartSaveDialog
			bind:open={saving}
			heading="Save as a smart collection"
			about="It holds what these filters find, in this order, kept up to date as the library changes."
			placeholder="Recent comedies"
			save={keepCollection}
		/>
	{/if}
	{#if playRule && data.editingList}
		<Button size="sm" onclick={saveListRule}>Save to the playlist</Button>
	{:else if playRule && !data.editing}
		<Button variant="outline" size="sm" onclick={() => (keeping = true)}>
			Save as a smart playlist
		</Button>
		<SmartSaveDialog
			bind:open={keeping}
			heading="Save as a smart playlist"
			about="It plays what these filters find, in this order, worked out again each time you open it."
			placeholder="Not yet seen"
			save={keepPlaylist}
		/>
	{/if}
	<WallFilterMenu query={data.query} {facets} onchange={show} />
	<WallSortMenu
		query={data.query}
		sites={facets.rating_sites}
		onchange={show}
	/>
	<ToggleGroup.Root
		type="single"
		variant="outline"
		size="sm"
		value={view}
		onValueChange={choose}
		aria-label="Show as"
	>
		<ToggleGroup.Item value="poster" aria-label="Posters">
			<LayoutGridIcon />
		</ToggleGroup.Item>
		<ToggleGroup.Item value="still" aria-label="Stills">
			<RectangleHorizontalIcon />
		</ToggleGroup.Item>
		<ToggleGroup.Item value="list" aria-label="List">
			<ListIcon />
		</ToggleGroup.Item>
	</ToggleGroup.Root>
</div>

<svelte:boundary>
	{@const answer = await shown}
	{#if "refused" in answer}
		<div class="grid min-h-[30svh] place-content-center gap-3 text-center">
			<p class="heading">{answer.refused}</p>
		</div>
	{:else if answer.titles.total}
		<div class="flex flex-col gap-4 md:flex-row-reverse">
			{#if answer.letters}
				{@const letters = answer.letters}
				<LetterBar
					{letters}
					onjump={(letter: string) => jump(letters, letter)}
				/>
			{/if}
			<div
				class={[
					"min-w-0 flex-1 transition-opacity",
					$effect.pending() && "opacity-60",
				]}
			>
				<Wall
					bind:this={wall}
					total={answer.titles.total}
					first={answer.titles.items}
					pageSize={wallPageSize}
					{fetchPage}
					{view}
					label={data.library.name}
				/>
			</div>
		</div>
	{:else}
		<div class="grid min-h-[30svh] place-content-center gap-3 text-center">
			{#if narrowed}
				<p class="heading">Nothing matches these filters</p>
				<p>
					<Button variant="outline" onclick={() => show(cleared(data.query))}>
						Clear filters
					</Button>
				</p>
			{:else}
				<p class="heading">Nothing in {data.library.name} yet</p>
				<p class="text-sm">Titles appear here as the library is scanned.</p>
			{/if}
		</div>
	{/if}
	{#snippet pending()}
		<ul
			class={view === "list" ? "grid gap-y-4.5" : "grid gap-x-3 gap-y-4.5"}
			style={view === "list"
				? ""
				: `grid-template-columns: ${gridColumns(view === "still" ? "still" : "poster")}`}
			aria-hidden="true"
		>
			{#each blanks as card (card)}
				<li>
					<div
						class={[
							"bg-raise rounded-xl",
							view === "list"
								? "h-18 w-full"
								: view === "still"
									? "aspect-video"
									: "aspect-2/3",
						]}
					></div>
				</li>
			{/each}
		</ul>
	{/snippet}
</svelte:boundary>
