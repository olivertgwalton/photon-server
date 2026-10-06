<script lang="ts">
import VolumeXIcon from "@lucide/svelte/icons/volume-x";
import { Button } from "#lib/components/ui/button/index.js";

// A title's theme tunes, played once under its page, one after another, as
// Jellyfin's web client plays theme songs. Leaving the page takes the element
// away, which stops it.
let { themes }: { themes: string[] } = $props();

// Quiet enough to read over, faded in and out over a couple of seconds.
const volume = 0.3;
const fadeMS = 2000;

let index = $state(0);
let stopped = $state(false);
let playing = $state(false);
let player = $state<HTMLAudioElement>();

$effect(() => {
	if (!player) return;
	player.volume = 0;
	// A browser that refuses to play before the reader has touched the page
	// leaves the page silent, as it should.
	player.play().catch(() => {});
});

// The volume follows the playhead: up from nothing at a tune's start, down to
// nothing at its end.
function shape() {
	if (!player || !Number.isFinite(player.duration)) return;
	const fromStart = player.currentTime * 1000;
	const toEnd = (player.duration - player.currentTime) * 1000;
	player.volume =
		volume * Math.max(0, Math.min(1, fromStart / fadeMS, toEnd / fadeMS));
}
</script>

{#if !stopped && index < themes.length}
	{#key index}
		<!-- biome-ignore lint/a11y/useMediaCaption: a theme tune is music, with no words to caption -->
		<audio
			bind:this={player}
			src="/api/v1/themes/{themes[index]}"
			preload="auto"
			data-theme-tune
			ontimeupdate={shape}
			onplaying={() => (playing = true)}
			onended={() => {
				playing = false;
				index++;
			}}
		></audio>
	{/key}
	{#if playing}
		<Button
			variant="outline"
			size="icon-lg"
			aria-label="Stop theme music"
			title="Stop theme music"
			onclick={() => (stopped = true)}
		>
			<VolumeXIcon />
		</Button>
	{/if}
{/if}
