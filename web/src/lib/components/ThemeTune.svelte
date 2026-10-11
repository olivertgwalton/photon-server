<script lang="ts">
import VolumeXIcon from "@lucide/svelte/icons/volume-x";
import type { Attachment } from "svelte/attachments";
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

// Each tune in turn, played from silence. A browser that refuses to play
// before the reader has touched the page leaves the page silent, as it should.
function plays(src: string): Attachment<HTMLAudioElement> {
	return (player) => {
		player.src = src;
		player.volume = 0;
		player.play().catch(() => {});
	};
}

// The volume follows the playhead: up from nothing at a tune's start, down to
// nothing at its end.
function shape(player: HTMLAudioElement) {
	if (!Number.isFinite(player.duration)) return;
	const fromStart = player.currentTime * 1000;
	const toEnd = (player.duration - player.currentTime) * 1000;
	player.volume =
		volume * Math.max(0, Math.min(1, fromStart / fadeMS, toEnd / fadeMS));
}
</script>

{#if !stopped && index < themes.length}
	<!-- biome-ignore lint/a11y/useMediaCaption: a theme tune is music, with no words to caption -->
	<audio
		{@attach plays(`/api/v1/themes/${themes[index]}`)}
		preload="auto"
		data-theme-tune
		ontimeupdate={(e) => shape(e.currentTarget)}
		onplaying={() => (playing = true)}
		onended={() => {
			playing = false;
			index++;
		}}
	></audio>
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
