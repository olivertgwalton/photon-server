<script lang="ts">
import AudioLinesIcon from "@lucide/svelte/icons/audio-lines";
import { listening } from "#lib/player/listening.svelte.ts";

// The mark of the track playing, its bars rising and falling while it plays,
// as Apple Music's and Spotify's do, and still while it is paused or where
// the reader asks for less motion.
let { class: className = "" }: { class?: string } = $props();
</script>

<span
	class={["bars inline-flex", !listening.playing && "paused", className]}
	role="img"
	aria-label={listening.playing ? "Playing" : "Paused"}
>
	<AudioLinesIcon class="size-full" aria-hidden="true" />
</span>

<style>
.bars :global(path) {
	transform-box: fill-box;
	transform-origin: center;
	animation: rise 0.7s ease-in-out infinite alternate;
}
.bars :global(path:nth-child(2)) {
	animation-delay: -0.23s;
}
.bars :global(path:nth-child(3)) {
	animation-delay: -0.47s;
}
.bars :global(path:nth-child(4)) {
	animation-delay: -0.12s;
}
.bars :global(path:nth-child(5)) {
	animation-delay: -0.35s;
}
.bars :global(path:nth-child(6)) {
	animation-delay: -0.58s;
}
.paused :global(path) {
	animation-play-state: paused;
}
@media (prefers-reduced-motion: reduce) {
	.bars :global(path) {
		animation: none;
	}
}
@keyframes rise {
	from {
		transform: scaleY(0.35);
	}
	to {
		transform: scaleY(1);
	}
}
</style>
