<script lang="ts">
import { prefersReducedMotion } from "svelte/motion";
import type { components } from "#lib/api/schema.js";
import ShowcaseSlide from "#lib/components/ShowcaseSlide.svelte";
import type { CarouselAPI } from "#lib/components/ui/carousel/context.js";
import * as Carousel from "#lib/components/ui/carousel/index.js";

// Home's showcase: the title the reader rested on, or a row they chose, which
// slides through itself, as the apps' carousel turns, a few seconds after
// each title comes to rest. It holds still from the first time the reader
// takes it in hand, while the pointer is over it, and for a reader who asks
// for less motion.
let { cards }: { cards: components["schemas"]["Card"][] } = $props();

const turnMs = 8_000;

let api = $state<CarouselAPI>();
let selected = $state(0);
let held = $state(false);
let over = $state(false);

$effect(() => {
	if (!api) return;
	const select = () => {
		selected = api?.selectedScrollSnap() ?? 0;
	};
	const take = () => {
		held = true;
	};
	api.on("select", select).on("pointerDown", take);
	return () => api?.off("select", select).off("pointerDown", take);
});

$effect(() => {
	selected;
	if (!api || held || over || prefersReducedMotion.current || cards.length < 2)
		return;
	const turn = setTimeout(() => api?.scrollNext(), turnMs);
	return () => clearTimeout(turn);
});
</script>

<!-- To the page's edges and its top, as a title's page draws its backdrop. -->
<section aria-label="Showcase" class="-mx-3 -mt-6 min-w-0 sm:-mx-6">
	{#if cards.length === 1}
		<ShowcaseSlide card={cards[0]} />
	{:else}
		<Carousel.Root
			opts={{ loop: true }}
			setApi={(a) => {
				api = a;
			}}
			onfocusin={() => {
				held = true;
			}}
			onpointerenter={() => {
				over = true;
			}}
			onpointerleave={() => {
				over = false;
			}}
		>
			<Carousel.Content class="ms-0">
				{#each cards as card, i (card.id)}
					<Carousel.Item
						class="ps-0"
						aria-label="{i + 1} of {cards.length}"
						inert={i !== selected}
					>
						<ShowcaseSlide {card} />
					</Carousel.Item>
				{/each}
			</Carousel.Content>
			<div
				class="absolute right-3 bottom-6 hidden gap-2 pointer-fine:flex sm:right-6"
			>
				<Carousel.Previous
					variant="glass"
					class="static"
					onclick={() => {
						held = true;
						api?.scrollPrev();
					}}
				/>
				<Carousel.Next
					variant="glass"
					class="static"
					onclick={() => {
						held = true;
						api?.scrollNext();
					}}
				/>
			</div>
		</Carousel.Root>
	{/if}
</section>
