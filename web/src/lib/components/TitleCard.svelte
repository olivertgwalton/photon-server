<script lang="ts">
import CheckIcon from "@lucide/svelte/icons/check";
import HeartIcon from "@lucide/svelte/icons/heart";
import { artworkSrc, artworkSrcset, type Shape } from "#lib/artwork.js";
import type { components } from "#lib/api/schema.js";
import { episodeLabel } from "#lib/format.js";
import TitleMenu from "./TitleMenu.svelte";

type Card = components["schemas"]["Card"];

// A title on a wall or a rail: a poster, or for an episode or a row about
// where the reader is, a still. `sizes` is how wide the rail draws it, and
// `caption` replaces the line under the name (a part played, a date).
let {
	card,
	shape = "poster",
	sizes,
	caption: given,
}: { card: Card; shape?: Shape; sizes: string; caption?: string } = $props();

const picture = $derived(
	shape === "poster"
		? (card.poster ?? card.thumb)
		: (card.thumb ?? card.backdrop),
);

// An episode is named by its show, with where it is in it beneath.
const name = $derived(card.show?.title ?? card.title);
const caption = $derived.by(() => {
	if (given !== undefined) return given;
	if (card.kind !== "episode") return card.year ? String(card.year) : "";
	const at = episodeLabel(
		card.season_number,
		card.episode_number,
		card.episode_end,
	);
	return [at, card.title].filter(Boolean).join(" · ");
});

const progress = $derived(
	card.state?.position_ms && card.duration_ms
		? Math.min(card.state.position_ms / card.duration_ms, 1)
		: 0,
);
const unwatched = $derived(card.state?.unwatched ?? 0);
// Watched again part way, it is the progress that shows.
const watched = $derived(!!card.state?.watched_at && !progress);
</script>

<div class="group/card relative">
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
			<div class="absolute top-2 left-2 flex gap-1">
				{#if watched}
					<span
						class="bg-ink text-ground grid size-6 place-items-center rounded-full"
						title="Watched"
					>
						<CheckIcon class="size-3.5" aria-hidden="true" />
						<span class="sr-only">Watched</span>
					</span>
				{:else if unwatched}
					<span
						class="bg-ink text-ground grid h-6 min-w-6 place-items-center rounded-full px-1.5 font-mono text-xs font-bold"
					>
						{unwatched}
						<span class="sr-only">unwatched</span>
					</span>
				{/if}
				{#if card.state?.favourite_at}
					<span
						class="bg-ground/80 text-ink grid size-6 place-items-center rounded-full"
						title="Favourite"
					>
						<HeartIcon class="size-3.5 fill-current" aria-hidden="true" />
						<span class="sr-only">Favourite</span>
					</span>
				{/if}
			</div>
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
	<!-- Beside the link, not in it: a control inside a link is two at once. -->
	<TitleMenu
		{card}
		class="absolute top-2 right-2 opacity-0 group-focus-within/card:opacity-100 group-hover/card:opacity-100 data-[state=open]:opacity-100 pointer-coarse:opacity-100"
	/>
</div>
