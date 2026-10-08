<script lang="ts">
import { vocabulary } from "#lib/vocabulary.js";
import CardGrid from "#lib/components/CardGrid.svelte";
import { byKind } from "#lib/rows.js";

let { data } = $props();
const words = vocabulary();

const groups = $derived(byKind(data.cards, words.kinds));
</script>

<svelte:head><title>Favourites · Photon</title></svelte:head>

<div class="grid gap-8">
	<h1 class="title">Favourites</h1>
	{#each groups as group (group.kind)}
		<section aria-labelledby="favourite-{group.kind}">
			<h2 id="favourite-{group.kind}" class="heading mb-3">{group.name}</h2>
			<CardGrid
				cards={group.cards}
				shape={group.kind === "episode" ? "still" : "poster"}
			/>
		</section>
	{:else}
		<p class="text-ink-3">
			Nothing here yet. Choose the heart on a title to keep it here.
		</p>
	{/each}
</div>
