<script lang="ts">
import type { Attachment } from "svelte/attachments";
import { innerHeight } from "svelte/reactivity/window";
import type { components } from "#lib/api/schema.js";
import { columnGap, columnsFor, gridColumns } from "#lib/grid.js";
import { onResize } from "#lib/size.js";
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

// The pages drawn, and each asked for, by number, until it arrives or is
// refused. A new `first` starts them again; a page answered for the wall
// before it is dropped.
let wall = $derived({
	pages: { 0: first } as Record<number, Card[]>,
	asked: new Map<number, Promise<void>>([[0, Promise.resolve()]]),
});

function load(page: number): Promise<void> {
	const { asked } = wall;
	const known = asked.get(page);
	if (known) return known;
	const loading = fetchPage(page * pageSize).then((cards) => {
		if (asked !== wall.asked) return;
		if (cards) wall = { asked, pages: { ...wall.pages, [page]: cards } };
		else asked.delete(page);
	});
	asked.set(page, loading);
	return loading;
}

let list = $state<HTMLElement>();
let width = $state(0);
// Where the wall's top is against the window's.
let top = $state(0);

function measure() {
	if (list) top = list.getBoundingClientRect().top;
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

// The rows drawn, as numbers: a scroll that keeps them changes nothing past
// here, where the cards would otherwise be gone over again on every one.
const screen = $derived(innerHeight.current || 800);
const from = $derived(
	Math.max(0, Math.floor((-top - screen * overscan) / rowHeight)),
);
const to = $derived(
	Math.min(rows, Math.ceil((-top + screen * (1 + overscan)) / rowHeight)),
);

const shown = $derived.by(() => {
	if (!width) return [];
	const out: number[] = [];
	for (let i = from * columns; i < Math.min(total, to * columns); i++) {
		out.push(i);
	}
	return out;
});

function card(index: number): Card | undefined {
	return wall.pages[Math.floor(index / pageSize)]?.[index % pageSize];
}

$effect(() => {
	for (const index of shown) load(Math.floor(index / pageSize));
});

// The card a letter jumped to, until it is drawn and has taken focus.
let jumped = $state<number>();

const focusCard: Attachment<HTMLElement> = (li) => {
	li.querySelector("a")?.focus({ preventScroll: true });
	jumped = undefined;
};

// Brings the card at `offset` to the top of the window and hands it focus,
// for the letters beside the wall.
export function jump(offset: number) {
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
	jumped = offset;
}

const sizes = $derived(`${Math.ceil(cardWidth) || 200}px`);
</script>

<svelte:window onscroll={measure} onresize={measure} />

{#snippet item(card: Card)}
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
				{@attach index === jumped && c ? focusCard : undefined}
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
				{:else}
					<div
						class={[
							"bg-raise rounded-xl",
							view === "list"
								? "h-18"
								: shape === "poster"
									? "aspect-2/3"
									: "aspect-video",
						]}
					></div>
				{/if}
			</li>
		{/each}
	{:else}
		{#each first as c (c.id)}
			<li class={view === "list" ? "h-20" : ""}>{@render item(c)}</li>
		{/each}
	{/if}
</ul>
