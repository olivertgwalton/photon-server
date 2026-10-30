<script lang="ts">
import LayoutGridIcon from "@lucide/svelte/icons/layout-grid";
import PlayIcon from "@lucide/svelte/icons/play";
import type { components } from "#lib/api/schema.d.ts";
import { Button } from "#lib/components/ui/button/index.ts";
import { episodeLabel } from "#lib/format.ts";

// The whole player covered, a couple of minutes into an episode nobody has
// answered for, with the picture paused under it: the question, what is
// playing, and the two answers, as the Photon apps ask it.
let {
	title,
	onkeep,
	onleave,
}: {
	title: components["schemas"]["TitlePage"];
	onkeep: () => void;
	onleave: () => void;
} = $props();

const id = $props.id();
// Keeping on is the answer a press of Enter gives, as the apps focus it.
let keep = $state<HTMLElement | null>(null);
$effect(() => keep?.focus());
const where = $derived(
	episodeLabel(title.season_number, title.episode_number, title.episode_end),
);
</script>

<svelte:window
	onkeydown={(e) => {
		if (e.key !== "Escape") return;
		e.preventDefault();
		onleave();
	}}
/>

<div
	role="alertdialog"
	aria-modal="true"
	aria-labelledby="{id}-question"
	class="bg-ground/90 absolute inset-0 z-10 grid place-items-center p-6 text-center backdrop-blur-sm"
>
	<div class="grid max-w-lg justify-items-center gap-6">
		<h1 id="{id}-question" class="title">Are you still watching?</h1>
		<div class="grid gap-1">
			{#if where}
				<p class="label">{where}</p>
			{/if}
			<p class="text-ink line-clamp-2 text-lg font-semibold">
				{[title.show?.title, title.title].filter(Boolean).join(" · ")}
			</p>
		</div>
		<div class="flex flex-wrap justify-center gap-3">
			<Button bind:ref={keep} size="lg" class="rounded-full" onclick={onkeep}>
				<PlayIcon class="size-4" />
				Continue Watching
			</Button>
			<Button
				size="lg"
				variant="outline"
				class="rounded-full"
				onclick={onleave}
			>
				<LayoutGridIcon class="size-4" />
				Back to Browse
			</Button>
		</div>
	</div>
</div>
