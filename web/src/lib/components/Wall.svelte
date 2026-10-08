<script lang="ts">
import { tick, untrack } from "svelte";
import { onResize } from "#lib/size.js";
import type { components } from "#lib/api/schema.js";
import { Skeleton } from "#lib/components/ui/skeleton/index.js";
import { columnGap, columnsFor, gridColumns } from "#lib/grid.js";
import type { ViewStyle } from "#lib/wall.js";
import TitleCard from "./TitleCard.svelte";
import TitleRow from "./TitleRow.svelte";

type Card = components["schemas"]["Card"];

// A shelf of any length, drawn a screen at a time and asked for a page at a
// time. `first` is the page the server drew; a new one (a new filter, or a
// change the server told of) starts the wall again where the reader is.
let {
	total,
	first,
	pageSize,
	fetchPage,
	view,
	label,
}: {
	total: number;
	first: Card[];
	pageSize: number;
	fetchPage: (offset: number) => Promise<Card[] | undefined>;
	view: ViewStyle;
	label: string;
} = $props();

// A screen above and below what shows, so a card is there before it is reached.
const overscan = 1;
const rowGap = 18;
const listRowHeight = 80;

let pages = $state<Record<number, Card[]>>({});
// Each page asked for, by number, until it arrives or is refused.
let asked = new Map<number, Promise<void>>();
let generation = 0;

$effect.pre(() => {
	const page = first;
	untrack(() => {
		generation++;
		asked = new Map([[0, Promise.resolve()]]);
		pages = { 0: page };
	});
});

function load(page: number): Promise<void> {
	const known = asked.get(page);
	if (known) return known;
	const asking = generation;
	const loading = fetchPage(page * pageSize).then((cards) => {
		if (asking !== generation) return;
		if (cards) pages[page] = cards;
		else asked.delete(page);
	});
	asked.set(page, loading);
	return loading;
}

let list = $state<HTMLElement>();
let width = $state(0);
// Where the wall's top is against the window's, and how much window there is.
let top = $state(0);
let viewport = $state(0);

function measure() {
	if (!list) return;
	top = list.getBoundingClientRect().top;
	viewport = window.innerHeight;
}

const resized = onResize((_, box) => {
	width = box.width;
	measure();
});

const shape = $derived(view === "still" ? "still" : "poster");
const columns = $derived(view === "list" ? 1 : columnsFor(width, shape));
const cardWidth = $derived((width - columnGap * (columns - 1)) / columns);
const rowHeight = $derived(
	view === "list"
		? listRowHeight
		: cardWidth * (shape === "poster" ? 1.5 : 9 / 16) + rowGap,
);
const rows = $derived(Math.ceil(total / columns));

const shown = $derived.by(() => {
	if (!width) return [];
	const screen = viewport || 800;
	const from = Math.max(0, Math.floor((-top - screen * overscan) / rowHeight));
	const to = Math.min(
		rows,
		Math.ceil((-top + screen * (1 + overscan)) / rowHeight),
	);
	const out: number[] = [];
	for (let i = from * columns; i < Math.min(total, to * columns); i++) {
		out.push(i);
	}
	return out;
});

function card(index: number): Card | undefined {
	return pages[Math.floor(index / pageSize)]?.[index % pageSize];
}

$effect(() => {
	for (const index of shown) load(Math.floor(index / pageSize));
});

// Brings the card at `offset` to the top of the window and hands it focus,
// for the letters beside the wall.
export async function jump(offset: number) {
	if (!list) return;
	const row = Math.floor(offset / columns);
	const header =
		document.querySelector("header")?.getBoundingClientRect().height ?? 0;
	window.scrollTo({
		top:
			window.scrollY +
			list.getBoundingClientRect().top +
			row * rowHeight -
			header -
			8,
	});
	measure();
	await load(Math.floor(offset / pageSize));
	await tick();
	list.querySelector<HTMLElement>(`[data-index="${offset}"] a`)?.focus({
		preventScroll: true,
	});
}

const sizes = $derived(`${Math.ceil(cardWidth) || 200}px`);
</script>

<svelte:window onscroll={measure} onresize={measure} />

{#snippet item(
	card: Card,
)}
	{#if view === "list"}
		<TitleRow {card} />
	{:else}
		<TitleCard {card} {shape} {sizes} />
	{/if}
{/snippet}

<ul
	bind:this={list}
	{@attach resized}
	aria-label={label}
	class={width ? "relative" : "grid gap-x-3 gap-y-4.5"}
	style={width
		? `height: ${Math.max(0, rows * rowHeight - (view === "list" ? 0 : rowGap))}px`
		: view === "list"
			? ""
			: `grid-template-columns: ${gridColumns(shape)}`}
>
	{#if width}
		{#each shown as index (index)}
			{@const c = card(index)}
			<li
				data-index={index}
				aria-setsize={total}
				aria-posinset={index + 1}
				class="absolute"
				style="top: {Math.floor(index / columns) * rowHeight}px; left: {(index %
					columns) *
					(cardWidth + columnGap)}px; width: {cardWidth}px; height: {rowHeight -
					(view === "list" ? 0 : rowGap)}px"
			>
				{#if c}
					{@render item(c)}
				{:else if view === "list"}
					<Skeleton class="h-18 w-full" />
				{:else}
					<Skeleton
						class={[
							"rounded-xl",
							shape === "poster" ? "aspect-[2/3]" : "aspect-video",
						]}
					/>
				{/if}
			</li>
		{/each}
	{:else}
		{#each first as c (c.id)}
			<li class={view === "list" ? "h-20" : ""}>{@render item(c)}</li>
		{/each}
	{/if}
</ul>
