<script lang="ts">
import { artworkSrc, artworkSrcset, type Shape } from "#lib/artwork.js";
import type { components } from "#lib/api/schema.js";

type Card = components["schemas"]["Card"];

// A title on a wall or a rail: a poster, or for an episode or a row about
// where the reader is, a still. `sizes` is how wide the rail draws it.
let {
	card,
	shape = "poster",
	sizes,
}: { card: Card; shape?: Shape; sizes: string } = $props();

const picture = $derived(
	shape === "poster" ? card.poster : (card.thumb ?? card.backdrop),
);

// An episode is named by its show, with where it is in it beneath.
const name = $derived(card.show?.title ?? card.title);
const caption = $derived.by(() => {
	if (card.kind !== "episode") return card.year ? String(card.year) : "";
	const at =
		card.season_number != null && card.episode_number != null
			? `S${card.season_number} E${card.episode_number}`
			: "";
	return [at, card.title].filter(Boolean).join(" · ");
});

const progress = $derived(
	card.state?.position_ms && card.duration_ms
		? Math.min(card.state.position_ms / card.duration_ms, 1)
		: 0,
);
</script>

<a href="/titles/{card.id}" class="group block outline-none">
	<div
		class={[
			"bg-raise group-hover:ring-line-strong group-focus-visible:ring-signal relative overflow-hidden rounded-lg ring-2 ring-transparent transition-shadow",
			shape === "poster" ? "aspect-[2/3]" : "aspect-video",
		]}
	>
		{#if picture}
			<img
				src={artworkSrc(picture, shape)}
				srcset={artworkSrcset(picture, shape)}
				{sizes}
				alt=""
				loading="lazy"
				decoding="async"
				class="size-full object-cover"
			>
		{:else}
			<span
				class="font-heading text-ink-3 grid size-full place-items-center p-3 text-center text-sm font-bold"
				aria-hidden="true"
			>
				{name}
			</span>
		{/if}
		{#if progress > 0}
			<div class="absolute inset-x-2 bottom-2 h-1 rounded-full bg-black/60">
				<div
					class="bg-ink h-full rounded-full"
					style="width: {progress * 100}%"
				></div>
			</div>
		{/if}
	</div>
	<p class="text-ink mt-2 truncate text-sm font-semibold">{name}</p>
	{#if caption}
		<p class="text-ink-3 truncate text-xs">{caption}</p>
	{/if}
</a>
