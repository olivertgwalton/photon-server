<script lang="ts">
import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
import type { Shape } from "#lib/artwork.js";
import type { components } from "#lib/api/schema.js";
import TitleCard from "./TitleCard.svelte";

// A named row of titles that scrolls sideways. With `href`, its heading leads
// to the whole of it.
let {
	title,
	cards,
	shape = "poster",
	href,
}: {
	title: string;
	cards: components["schemas"]["Card"][];
	shape?: Shape;
	href?: string;
} = $props();

const id = $props.id();

// Each card's width, and what its picture is asked for at.
const width = $derived(
	shape === "poster"
		? "w-32 sm:w-36 lg:w-40 2xl:w-48"
		: "w-60 sm:w-64 lg:w-72 2xl:w-80",
);
const sizes = $derived(
	shape === "poster"
		? "(min-width: 1536px) 12rem, (min-width: 1024px) 10rem, 9rem"
		: "(min-width: 1536px) 20rem, (min-width: 1024px) 18rem, 16rem",
);
</script>

<section aria-labelledby={id}>
	<h2 {id} class="heading mb-3">
		{#if href}
			<a {href} class="group inline-flex items-center gap-1 hover:underline">
				{title}
				<ChevronRightIcon
					class="text-ink-3 group-hover:text-ink size-5"
					aria-hidden="true"
				/>
			</a>
		{:else}
			{title}
		{/if}
	</h2>
	<ul
		class="-mx-3 flex snap-x scroll-px-3 gap-3 overflow-x-auto px-3 pb-2 sm:-mx-6 sm:scroll-px-6 sm:gap-4 sm:px-6"
	>
		{#each cards as card (card.id)}
			<li class="shrink-0 snap-start {width}">
				<TitleCard {card} {shape} {sizes} />
			</li>
		{/each}
	</ul>
</section>
