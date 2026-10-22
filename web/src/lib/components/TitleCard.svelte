<script lang="ts" module>
import type { components } from "#lib/api/schema.js";

type Card = components["schemas"]["Card"];
// A card names its title; everything else it shows is what it has.
export type CardLike = Pick<Card, "id" | "kind" | "title"> & Partial<Card>;
</script>

<script lang="ts">
import CheckIcon from "@lucide/svelte/icons/check";
import HeartIcon from "@lucide/svelte/icons/heart";
import PlayIcon from "@lucide/svelte/icons/play";
import type { Shape } from "#lib/artwork.js";
import { blurStyle } from "#lib/blurhash.js";
import Artwork from "#lib/components/Artwork.svelte";
import { episodeLine } from "#lib/format.js";
import { hidesSpoiler } from "#lib/spoilers.js";
import TitleMenuButton from "./TitleMenuButton.svelte";

// A title on a wall or a rail: a poster, or for an episode or a row about
// where the reader is, a still. `sizes` is how wide the rail draws it, and
// `caption` replaces the line under the name (a part played, a date).
// A title the server does not hold has no title's menu.
let {
	card,
	shape = "poster",
	sizes,
	caption: given,
	current = false,
	menu = "title",
}: {
	card: CardLike;
	shape?: Shape;
	sizes: string;
	caption?: string;
	// The page's own title, outlined among the rest of its set.
	current?: boolean;
	menu?: "title" | "none";
} = $props();

// An episode drawn as a poster wears its season's art, as Infuse draws one
// among a search's results, not the show's poster its card otherwise carries.
const picture = $derived(
	shape === "poster"
		? (card.season_poster ?? card.poster ?? card.thumb)
		: (card.thumb ?? card.backdrop),
);

// An episode or a season is named by its show, with where it is in it beneath.
const name = $derived(card.show?.title ?? card.title);
const caption = $derived.by(() => {
	if (given !== undefined) return given;
	if (card.kind === "season") return card.title;
	if (card.kind !== "episode") return card.year ? String(card.year) : "";
	return episodeLine(card);
});

const progress = $derived(
	card.state?.position_ms && card.duration_ms
		? Math.min(card.state.position_ms / card.duration_ms, 1)
		: 0,
);
const unwatched = $derived(card.state?.unwatched ?? 0);
// Drawn as a poster, an episode wears its season's art, which tells nothing.
const blurred = $derived(shape !== "poster" && hidesSpoiler(card));
// Watched again part way, it is the progress that shows.
const watched = $derived(!!card.state?.watched_at && !progress);
</script>

<div class="group/card relative">
	<!-- The words sit over the picture, as the app's cards have them, so a row
		is the height of its pictures. The blur is the frame's, under a picture
		that fades in over it. -->
	<a
		href="/titles/{card.id}"
		aria-current={current ? "page" : undefined}
		class="group block outline-none"
	>
		<div
			class={[
				"card-frame @container",
				current && "ring-ink ring-2",
				shape === "poster" ? "aspect-2/3" : "aspect-video",
			]}
			style={picture ? blurStyle(card.blurhashes?.[picture]) : undefined}
		>
			{#if picture}
				<Artwork
					id={picture}
					{shape}
					{sizes}
					class={["card-picture", blurred && "spoiler-blur"]}
				/>
			{/if}
			<div class="absolute top-2 left-2 flex gap-1">
				{#if watched}
					<span class="glass-disc" title="Watched">
						<CheckIcon class="size-3.5" aria-hidden="true" />
						<span class="sr-only">Watched</span>
					</span>
				{:else if unwatched}
					<span
						class="glass-disc w-auto min-w-6 px-1.5 font-mono text-xs font-bold"
					>
						{unwatched}
						<span class="sr-only">unwatched</span>
					</span>
				{/if}
				{#if card.state?.favourite_at}
					<span class="glass-disc" title="Favourite">
						<HeartIcon class="size-3.5 fill-current" aria-hidden="true" />
						<span class="sr-only">Favourite</span>
					</span>
				{/if}
			</div>
			<div class="card-shade grid gap-0.5">
				<p
					class="text-ink line-clamp-2 text-[0.8125rem] leading-tight font-semibold"
				>
					{name}
				</p>
				{#if progress > 0 || caption}
					<p class="text-ink-2 flex items-center gap-1.5 text-xs">
						{#if progress > 0}
							<PlayIcon
								class="size-2.5 shrink-0 fill-current"
								aria-hidden="true"
							/>
							<!-- Never empty: a play just begun still shows it has. -->
							<span class="h-1.25 w-7 shrink-0 rounded-full bg-white/35">
								<span
									class="bg-ink block h-full rounded-full"
									style="width: calc(5px + 23px * {progress})"
								></span>
							</span>
						{/if}
						<span class="truncate">{caption}</span>
					</p>
				{/if}
			</div>
		</div>
	</a>
	{#if menu === "title"}
		<!-- Beside the link, not in it: a control inside a link is two at once. -->
		<TitleMenuButton
			{card}
			class="absolute top-2 right-2 opacity-0 group-focus-within/card:opacity-100 group-hover/card:opacity-100 data-[state=open]:opacity-100 pointer-coarse:opacity-100"
		/>
	{/if}
</div>
