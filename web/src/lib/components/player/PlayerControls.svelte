<script lang="ts">
import PauseIcon from "@lucide/svelte/icons/pause";
import PlayIcon from "@lucide/svelte/icons/play";
import RepeatIcon from "@lucide/svelte/icons/repeat";
import Repeat1Icon from "@lucide/svelte/icons/repeat-1";
import ShuffleIcon from "@lucide/svelte/icons/shuffle";
import SkipBackIcon from "@lucide/svelte/icons/skip-back";
import SkipForwardIcon from "@lucide/svelte/icons/skip-forward";
import { Button } from "#lib/components/ui/button/index.ts";
import { Toggle } from "#lib/components/ui/toggle/index.ts";
import type { Repeat } from "#lib/player/listen.ts";
import { listening } from "#lib/player/listening.svelte.ts";

// Shuffle, the one before, play and pause, the next and repeat: the bar's at
// its size, the Now Playing view's larger.
let { size }: { size: "bar" | "large" } = $props();

const repeatLabels: Record<Repeat, string> = {
	off: "Repeat",
	all: "Repeating the queue",
	one: "Repeating this one",
};
const large = $derived(size === "large");
</script>

<div class={["flex items-center", large ? "gap-4" : "gap-1"]}>
	<Toggle
		size={large ? "lg" : "sm"}
		pressed={listening.order === "shuffled"}
		onPressedChange={listening.toggleShuffle}
		aria-label="Shuffle"
		class={[
			"text-ink-3 aria-pressed:text-signal aria-pressed:bg-transparent",
			!large && "max-sm:hidden",
		]}
	>
		<ShuffleIcon />
	</Toggle>
	<Button
		variant="ghost"
		size={large ? "icon-lg" : "icon"}
		aria-label="Previous"
		class={large ? "" : "max-sm:hidden"}
		onclick={listening.previous}
	>
		<SkipBackIcon class="fill-current" />
	</Button>
	<Button
		size={large ? "icon-lg" : "icon"}
		class={["rounded-full", large && "size-16 [&_svg]:size-7!"]}
		aria-label={listening.playing ? "Pause" : "Play"}
		onclick={listening.toggle}
	>
		{#if listening.playing}
			<PauseIcon class="fill-current" />
		{:else}
			<PlayIcon class="fill-current" />
		{/if}
	</Button>
	<Button
		variant="ghost"
		size={large ? "icon-lg" : "icon"}
		aria-label="Next"
		onclick={listening.next}
	>
		<SkipForwardIcon class="fill-current" />
	</Button>
	<Button
		variant="ghost"
		size={large ? "icon-lg" : "icon-sm"}
		aria-label={repeatLabels[listening.repeat]}
		aria-pressed={listening.repeat !== "off"}
		title={repeatLabels[listening.repeat]}
		class={["text-ink-3 aria-pressed:text-signal", !large && "max-sm:hidden"]}
		onclick={listening.cycleRepeat}
	>
		{#if listening.repeat === "one"}
			<Repeat1Icon />
		{:else}
			<RepeatIcon />
		{/if}
	</Button>
</div>
