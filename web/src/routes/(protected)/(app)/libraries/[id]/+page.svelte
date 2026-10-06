<script lang="ts">
import LayoutGridIcon from "@lucide/svelte/icons/layout-grid";
import ListIcon from "@lucide/svelte/icons/list";
import RectangleHorizontalIcon from "@lucide/svelte/icons/rectangle-horizontal";
import { goto } from "$app/navigation";
import { client } from "#lib/api/client.js";
import LetterBar from "#lib/components/LetterBar.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import { count } from "#lib/format.js";
import * as ToggleGroup from "#lib/components/ui/toggle-group/index.js";
import Wall from "#lib/components/Wall.svelte";
import WallFilterMenu from "#lib/components/WallFilterMenu.svelte";
import WallSortMenu from "#lib/components/WallSortMenu.svelte";
import {
	cleared,
	filterCount,
	letterOffset,
	type ViewStyle,
	viewKey,
	type WallQuery,
	wallPageSize,
	wallSearch,
} from "#lib/wall.js";

let { data } = $props();

let wall = $state<Wall>();
let view = $derived(data.view);

const id = $derived(data.library.id);
const narrowed = $derived(filterCount(data.query) > 0);

// The filters live in the address: a narrowed wall can be shared, and Back
// leaves the library rather than undoing a filter.
function show(query: WallQuery) {
	goto(`/libraries/${id}${wallSearch(query)}`, {
		replace: true,
		reset: false,
	});
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
	if (!data.letters) return;
	wall?.jump(letterOffset(data.letters, letter, data.query.order ?? "asc"));
}
</script>

<svelte:head><title>{data.library.name} · Photon</title></svelte:head>

<h2 class="sr-only">Titles</h2>
<div class="flex flex-wrap items-center gap-2">
	<p class="text-ink-3 mr-auto text-sm" aria-live="polite">
		{count(data.titles.total, "title")}
	</p>
	{#if narrowed}
		<Button variant="ghost" size="sm" onclick={() => show(cleared(data.query))}>
			Clear filters
		</Button>
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

{#if data.titles.total}
	<div class="flex flex-col gap-4 md:flex-row-reverse">
		{#if data.letters}
			<LetterBar letters={data.letters} onjump={jump} />
		{/if}
		<div class="min-w-0 flex-1">
			<Wall
				bind:this={wall}
				total={data.titles.total}
				first={data.titles.items}
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
