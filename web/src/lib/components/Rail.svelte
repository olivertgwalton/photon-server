<script lang="ts" generics="T extends Record<'id', string>">
import ChevronLeftIcon from "@lucide/svelte/icons/chevron-left";
import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
import type { Snippet } from "svelte";
import { on as listen } from "svelte/events";
import type { Shape } from "#lib/artwork.js";
import { railLimit } from "#lib/rows.js";
import { onResize } from "#lib/size.js";
import TitleCard, { type CardLike } from "./TitleCard.svelte";

// A named row of titles that scrolls sideways. Where it holds more than it
// shows (or `total` says there are more than it was given), its heading leads
// to the whole of it at `href`. Its `cards` are TitleCards; other `items` are
// drawn by `card`, told how wide the row draws them. A row with a `current`
// card is the whole of a set the page is one of (a season, on an episode's
// page): it shows all of it, scrolled to that card.
let {
	title,
	cards = [],
	items = [],
	card,
	shape = "poster",
	href,
	total,
	caption,
	current,
	heading,
}: {
	title: string;
	cards?: CardLike[];
	items?: T[];
	card?: Snippet<[T, string]>;
	shape?: Shape;
	href?: string;
	total?: number;
	caption?: (index: number) => string | undefined;
	current?: string;
	// What the row's name is drawn as, where it is more than its words.
	heading?: Snippet;
} = $props();

const shown = $derived(current ? cards : cards.slice(0, railLimit));

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

$effect(() => {
	if (!list || !current) return;
	const here = list.querySelector<HTMLElement>("[aria-current=page]");
	const li = here?.closest("li");
	if (!li) return;
	// Sideways only: scrollIntoView would also scroll the page down to the row.
	list.scrollTo({
		left:
			li.offsetLeft -
			Number.parseFloat(getComputedStyle(list).scrollPaddingLeft),
		behavior: "instant",
	});
	measure();
});

// A page at a time, as Plex Web's and Jellyfin's rows go: a little less than
// the row's width, so the card cut at the edge is the first one shown.
function page(direction: 1 | -1) {
	list?.scrollBy({ left: direction * list.clientWidth * 0.9 });
}

// Left and Right step from card to card, as a row goes on a TV: the next
// card's link takes focus, and the row scrolls to it. Heard on the list, from
// the links in it.
function step(event: KeyboardEvent) {
	const by = { ArrowLeft: -1, ArrowRight: 1 }[event.key];
	if (!by || !list) return;
	const cards = [...list.children];
	const at = cards.findIndex((li) => li.contains(document.activeElement));
	const next = cards[at + by]?.querySelector<HTMLElement>("a, button");
	if (at < 0 || !next) return;
	event.preventDefault();
	next.focus();
}

const sizes = $derived(
	shape === "poster"
		? "(min-width: 1536px) 12rem, (min-width: 1024px) 10rem, 9rem"
		: "(min-width: 1536px) 20rem, (min-width: 1024px) 18rem, 16rem",
);
</script>

{#snippet arrow(
	direction: 1 | -1,
	label: string,
	enabled: boolean,
)}
	<button
		type="button"
		onclick={() => page(direction)}
		disabled={!enabled}
		class="text-ink ring-line-strong hover:bg-raise focus-visible:outline-signal grid size-8 place-items-center rounded-full bg-white/6 ring-1 transition-[background-color,opacity] duration-200 disabled:opacity-35 disabled:hover:bg-white/6"
	>
		{#if direction < 0}
			<ChevronLeftIcon class="size-4" aria-hidden="true" />
		{:else}
			<ChevronRightIcon class="size-4" aria-hidden="true" />
		{/if}
		<span class="sr-only">{label} {title}</span>
	</button>
{/snippet}

<!-- min-w-0: in a grid or flex row, the cards would otherwise widen the page.
	The list is relative so what a card places absolutely (words for a screen
	reader) scrolls with it rather than past the page's edge. It scrolls sideways
	only and draws no scrollbar, even where scroll bars are always shown: a
	trackpad, a swipe, Tab from card to card and the arrows move it, and its
	padding holds a card's focus ring. The list, not the row, skips drawing
	while it is off screen: content-visibility clips to the box it is on, and
	the list's own box holds every card and its ring, where the row's would cut
	off what the list draws past it to the page's edges. -->
<section aria-labelledby={id} class="min-w-0">
	<!-- A row's name is quieter than its cards', as the app has it: the cards
		are what is read. Its arrows sit together at the other end of the line,
		over the row rather than on its cards. -->
	<div class="mb-3 flex items-center justify-between gap-3">
		<h2 {id} class="text-ink-2 font-sans text-[0.9375rem] font-semibold">
			{#if more}
				<a
					href={more}
					class="hover:text-ink inline-flex items-center gap-1 rounded-full bg-white/6 py-1 pr-2 pl-3 transition-colors duration-200 hover:bg-white/12"
				>
					{title}
					<ChevronRightIcon class="size-4" aria-hidden="true" />
					<span class="sr-only">View all</span>
				</a>
			{:else if heading}
				{@render heading()}
			{:else}
				{title}
			{/if}
		</h2>
		{#if back || on}
			<div class="hidden gap-2 pointer-fine:flex">
				{@render arrow(-1, "Previous in", back)}
				{@render arrow(1, "Next in", on)}
			</div>
		{/if}
	</div>
	<div class="relative">
		<!-- Picking another of the set the page is one of is moving within it, so
			the page keeps its scroll and the picked card its focus, rather than
			both going back to the top. -->
		<ul
			data-sveltekit-reset={current ? "false" : undefined}
			bind:this={list}
			{@attach resized}
			onscroll={measure}
			{@attach (ul) => listen(ul, "keydown", step)}
			class="relative -mx-3 flex [contain-intrinsic-size:auto_16rem] [content-visibility:auto] scroll-smooth snap-x snap-mandatory scroll-px-3 gap-3 overflow-x-auto overflow-y-hidden px-3 py-2 [scrollbar-width:none] sm:-mx-6 sm:scroll-px-6 sm:px-6 [&::-webkit-scrollbar]:hidden"
		>
			{#each shown as c, i (c.id)}
				<li class="shrink-0 snap-start {width}">
					<TitleCard
						card={c}
						{shape}
						{sizes}
						caption={caption?.(i)}
						current={c.id === current}
					/>
				</li>
			{/each}
			{#if card}
				{#each items.slice(0, railLimit) as item (item.id)}
					<li class="shrink-0 snap-start {width}">
						{@render card(item, sizes)}
					</li>
				{/each}
			{/if}
		</ul>
	</div>
</section>
