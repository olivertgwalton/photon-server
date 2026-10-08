<script lang="ts">
import LayoutGridIcon from "@lucide/svelte/icons/layout-grid";
import ListIcon from "@lucide/svelte/icons/list";
import RectangleHorizontalIcon from "@lucide/svelte/icons/rectangle-horizontal";
import { goto } from "$app/navigation";
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import SmartCollectionDialog from "#lib/components/admin/SmartCollectionDialog.svelte";
import LetterBar from "#lib/components/LetterBar.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import { Skeleton } from "#lib/components/ui/skeleton/index.js";
import * as ToggleGroup from "#lib/components/ui/toggle-group/index.js";
import Wall from "#lib/components/Wall.svelte";
import WallFilterMenu from "#lib/components/WallFilterMenu.svelte";
import WallSortMenu from "#lib/components/WallSortMenu.svelte";
import { count } from "#lib/format.js";
import { gridColumns } from "#lib/grid.js";
import { settled } from "#lib/settled.svelte.js";
import {
	cleared,
	filterCount,
	letterOffset,
	smartRule,
	type ViewStyle,
	viewKey,
	type WallQuery,
	wallPageSize,
	wallSearch,
} from "#lib/wall.js";

let { data } = $props();

let wall = $state<Wall>();
let view = $derived(data.view);
const streamed = settled(() => data.wall);
// The wall of this address: one of another query, still here while this one
// is on its way, is not drawn beside this one's filters.
const shown = $derived(
	streamed.value && wallSearch(streamed.value.query) === wallSearch(data.query)
		? streamed.value
		: undefined,
);
// Blank cards, a screenful, until the wall arrives.
const blanks = Array.from({ length: 24 }, (_, n) => n);

const id = $derived(data.library.id);
const narrowed = $derived(filterCount(data.query) > 0);
// What an admin may keep of this wall as a smart collection.
const rule = $derived(
	data.me.role === "admin" && (narrowed || data.query.sort)
		? smartRule(data.query)
		: undefined,
);
let saving = $state(false);

// The filters live in the address: a narrowed wall can be shared, and Back
// leaves the library rather than undoing a filter.
function show(query: WallQuery) {
	const search = wallSearch(query);
	const editing = data.editing
		? `${search ? "&" : "?"}collection=${data.editing}`
		: "";
	goto(`/libraries/${id}${search}${editing}`, {
		replace: true,
		reset: false,
	});
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

function jump(letter: string) {
	if (!shown?.letters) return;
	wall?.jump(letterOffset(shown.letters, letter, data.query.order ?? "asc"));
}
</script>

<svelte:head><title>{data.library.name} · Photon</title></svelte:head>

<h2 class="sr-only">Titles</h2>
<div class="flex flex-wrap items-center gap-2">
	<p class="text-ink-3 mr-auto text-sm" aria-live="polite">
		{#if shown}
			{count(shown.titles.total, "title")}
		{:else}
			<Skeleton class="inline-block h-4 w-16 align-middle" />
		{/if}
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
		<SmartCollectionDialog bind:open={saving} library={id} {rule} />
	{/if}
	<WallFilterMenu query={data.query} facets={data.facets} onchange={show} />
	<WallSortMenu
		query={data.query}
		sites={data.facets.rating_sites}
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

{#if streamed.failed}
	<div class="grid min-h-[30svh] place-content-center gap-3 text-center">
		<p class="heading">{streamed.failed}</p>
	</div>
{:else if !shown}
	<ul
		class={view === "list" ? "grid gap-y-4.5" : "grid gap-x-3 gap-y-4.5"}
		style={view === "list"
			? ""
			: `grid-template-columns: ${gridColumns(view === "still" ? "still" : "poster")}`}
		aria-hidden="true"
	>
		{#each blanks as card (card)}
			<li>
				<Skeleton
					class={[
						"rounded-xl",
						view === "list"
							? "h-18 w-full"
							: view === "still"
								? "aspect-video"
								: "aspect-[2/3]",
					]}
				/>
			</li>
		{/each}
	</ul>
{:else if shown.titles.total}
	<div class="flex flex-col gap-4 md:flex-row-reverse">
		{#if shown.letters}
			<LetterBar letters={shown.letters} onjump={jump} />
		{/if}
		<div class="min-w-0 flex-1">
			<Wall
				bind:this={wall}
				total={shown.titles.total}
				first={shown.titles.items}
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
