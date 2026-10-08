<script lang="ts" generics="T extends Record<'id', string>">
import ChevronLeftIcon from "@lucide/svelte/icons/chevron-left";
import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
import type { Snippet } from "svelte";
import type { Shape } from "#lib/artwork.js";
import { railLimit } from "#lib/rows.js";
import { onResize } from "#lib/size.js";
import TitleCard, { type CardLike } from "./TitleCard.svelte";

// A named row of titles that scrolls sideways. Where it holds more than it
// shows (or `total` says there are more than it was given), its heading leads
// to the whole of it at `href`. Its `cards` are TitleCards; other `items` are
// drawn by `card`, told how wide the row draws them.
let {
	title,
	cards = [],
	items = [],
	card,
	shape = "poster",
	href,
	total,
	caption,
}: {
	title: string;
	cards?: CardLike[];
	items?: T[];
	card?: Snippet<[T, string]>;
	shape?: Shape;
	href?: string;
	total?: number;
	caption?: (index: number) => string | undefined;
} = $props();

const more = $derived(
	(total ?? cards.length + items.length) > railLimit ? href : undefined,
);

const id = $props.id();

// Each card's width, and what its picture is asked for at.
const width = $derived(
	shape === "poster"
		? "w-32 sm:w-36 lg:w-40 2xl:w-48"
		: "w-60 sm:w-64 lg:w-72 2xl:w-80",
);
// Which ways there is more to scroll to, for the arrows.
let list = $state<HTMLUListElement>();
let back = $state(false);
let on = $state(false);
function measure() {
	if (!list) return;
	back = list.scrollLeft > 1;
	on = list.scrollLeft + list.clientWidth < list.scrollWidth - 1;
}
const resized = onResize(measure);

// A page at a time, as Plex Web's and Jellyfin's rows go: a little less than
// the row's width, so the card cut at the edge is the first one shown.
function page(direction: 1 | -1) {
	list?.scrollBy({ left: direction * list.clientWidth * 0.9 });
}

// Cards fade in one after another, the first screenful only. Only their
// opacity moves: their places are already kept.
const arrive =
	"animate-in fade-in fill-mode-both shrink-0 snap-start duration-500 ease-out-expo";
const stagger = (i: number) => `animation-delay: ${Math.min(i, 8) * 35}ms`;

const sizes = $derived(
	shape === "poster"
		? "(min-width: 1536px) 12rem, (min-width: 1024px) 10rem, 9rem"
		: "(min-width: 1536px) 20rem, (min-width: 1024px) 18rem, 16rem",
);
</script>

<!-- min-w-0: in a grid or flex row, the cards would otherwise widen the page.
	The list is relative so what a card places absolutely (words for a screen
	reader) scrolls with it rather than past the page's edge. It scrolls sideways
	only and draws no scrollbar, even where scroll bars are always shown: a
	trackpad, a swipe, Tab from card to card and the arrows move it, and its
	padding holds a card's focus ring. -->
<section
	aria-labelledby={id}
	class="min-w-0 [contain-intrinsic-size:auto_20rem] [content-visibility:auto]"
>
	<!-- A row's name is quieter than its cards', as the app has it: the cards
		are what is read. -->
	<h2 {id} class="text-ink-2 mb-3 font-sans text-[0.9375rem] font-semibold">
		{#if more}
			<a
				href={more}
				class="hover:text-ink inline-flex items-center gap-1 rounded-full bg-white/6 py-1 pr-2 pl-3 transition-colors duration-200 hover:bg-white/12"
			>
				{title}
				<ChevronRightIcon class="size-4" aria-hidden="true" />
				<span class="sr-only">View all</span>
			</a>
		{:else}
			{title}
		{/if}
	</h2>
	<div class="group/rail relative">
		<ul
			bind:this={list}
			{@attach resized}
			onscroll={measure}
			class="relative -mx-3 flex scroll-smooth snap-x snap-mandatory scroll-px-3 gap-3 overflow-x-auto overflow-y-hidden px-3 py-2 [scrollbar-width:none] sm:-mx-6 sm:scroll-px-6 sm:px-6 [&::-webkit-scrollbar]:hidden"
		>
			{#each cards.slice(0, railLimit) as c, i (c.id)}
				<li class="{arrive} {width}" style={stagger(i)}>
					<TitleCard card={c} {shape} {sizes} caption={caption?.(i)} />
				</li>
			{/each}
			{#if card}
				{#each items.slice(0, railLimit) as item, i (item.id)}
					<li class="{arrive} {width}" style={stagger(i)}>
						{@render card(item, sizes)}
					</li>
				{/each}
			{/if}
		</ul>
		{#snippet arrow(
			direction: 1 | -1,
			label: string,
		)}
			<button
				type="button"
				onclick={() => page(direction)}
				class={[
					"bg-ground/85 text-ink ring-line-strong hover:bg-raise focus-visible:outline-signal absolute top-1/2 z-10 hidden size-10 -translate-y-1/2 place-items-center rounded-full opacity-0 shadow-lg ring-1 backdrop-blur transition-opacity group-hover/rail:opacity-100 focus-visible:opacity-100 pointer-fine:grid",
					direction < 0 ? "-left-1 sm:-left-3" : "-right-1 sm:-right-3",
				]}
			>
				{#if direction < 0}
					<ChevronLeftIcon class="size-5" aria-hidden="true" />
				{:else}
					<ChevronRightIcon class="size-5" aria-hidden="true" />
				{/if}
				<span class="sr-only">{label} {title}</span>
			</button>
		{/snippet}
		{#if back}
			{@render arrow(-1, "Previous in")}
		{/if}
		{#if on}
			{@render arrow(1, "Next in")}
		{/if}
	</div>
</section>
