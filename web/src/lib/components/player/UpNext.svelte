<script lang="ts">
import { onDestroy } from "svelte";
import { artworkSrc } from "#lib/artwork.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";

// The next episode, offered as the credits roll, playing by itself once the
// count runs out unless the reader says otherwise. The count holds while the
// title is paused.
let {
	card,
	paused,
	onplay,
	ondismiss,
}: {
	card: components["schemas"]["Card"];
	paused: boolean;
	onplay: () => void;
	ondismiss: () => void;
} = $props();

const countdown = 10;
let left = $state(countdown);

const timer = setInterval(() => {
	if (paused) return;
	left -= 1;
	if (left <= 0) {
		clearInterval(timer);
		onplay();
	}
}, 1000);
onDestroy(() => clearInterval(timer));

const picture = $derived(card.thumb ?? card.backdrop);
const where = $derived(
	card.season_number != null && card.episode_number != null
		? `S${card.season_number} E${card.episode_number}`
		: "",
);
</script>

<section
	aria-labelledby="up-next"
	class="bg-raise/95 ring-line-strong flex w-80 max-w-[calc(100vw-2rem)] gap-3 rounded-xl p-3 shadow-2xl ring-1 backdrop-blur"
>
	<div class="bg-ground aspect-video w-28 shrink-0 overflow-hidden rounded-md">
		{#if picture}
			<img
				src={artworkSrc(picture, "still")}
				alt=""
				class="size-full object-cover"
			>
		{/if}
	</div>
	<div class="flex min-w-0 flex-col gap-2">
		<div class="min-w-0">
			<p id="up-next" class="label">Up next</p>
			<p class="text-ink truncate text-sm font-semibold">
				{[where, card.title].filter(Boolean).join(" · ")}
			</p>
			<p class="text-ink-3 text-xs tabular-nums">Plays in {left} s</p>
		</div>
		<div class="flex gap-2">
			<Button size="sm" onclick={onplay}>Play now</Button>
			<Button size="sm" variant="ghost" onclick={ondismiss}>Hide</Button>
		</div>
	</div>
</section>
