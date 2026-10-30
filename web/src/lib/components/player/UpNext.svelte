<script lang="ts">
import type { components } from "#lib/api/schema.d.ts";
import { blurStyle } from "#lib/blurhash.ts";
import Artwork from "#lib/components/Artwork.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import { episodeLabel } from "#lib/format.ts";
import type { Advance } from "#lib/player/stillwatching.ts";
import { hidesSpoiler } from "#lib/spoilers.ts";

// The next episode, offered as the credits roll, playing by itself once the
// count runs out unless the reader says otherwise, or has turned autoplay off.
// The count holds while the title is paused.
let {
	card,
	paused,
	autoplay,
	onplay,
	ondismiss,
}: {
	card: components["schemas"]["Card"];
	paused: boolean;
	autoplay: boolean;
	onplay: (by: Advance) => void;
	ondismiss: () => void;
} = $props();

const id = $props.id();

const countdown = 10;
let left = $state(countdown);

// It counts only while the title plays and autoplay is on.
$effect(() => {
	if (paused || !autoplay) return;
	const timer = setInterval(() => {
		left -= 1;
		if (left > 0) return;
		clearInterval(timer);
		onplay("itself");
	}, 1000);
	return () => clearInterval(timer);
});

const picture = $derived(card.thumb ?? card.backdrop);
const where = $derived(episodeLabel(card.season_number, card.episode_number));
</script>

<section
	aria-labelledby="{id}-up-next"
	class="bg-raise/95 ring-line-strong flex w-80 max-w-[calc(100vw-2rem)] gap-3 rounded-xl p-3 shadow-2xl ring-1 backdrop-blur"
>
	<div
		class="bg-ground aspect-video w-28 shrink-0 self-start overflow-hidden rounded-md"
		style={picture ? blurStyle(card.blurhashes?.[picture]) : undefined}
	>
		{#if picture && !hidesSpoiler(card)}
			<Artwork
				id={picture}
				shape="still"
				loading="eager"
				class="size-full object-cover"
			/>
		{/if}
	</div>
	<div class="flex min-w-0 flex-col gap-2">
		<div class="min-w-0">
			<p id="{id}-up-next" class="label">Up next</p>
			<p class="text-ink truncate text-sm font-semibold">
				{[where, card.title].filter(Boolean).join(" · ")}
			</p>
			{#if autoplay}
				<p class="text-ink-3 text-xs tabular-nums">Plays in {left} s</p>
			{/if}
		</div>
		<div class="flex gap-2">
			<Button size="sm" onclick={() => onplay("reader")}>Play now</Button>
			<Button size="sm" variant="ghost" onclick={ondismiss}>Hide</Button>
		</div>
	</div>
</section>
