<script lang="ts">
import CardGrid from "#lib/components/CardGrid.svelte";
import { byKind } from "#lib/rows.js";
import { vocabulary } from "#lib/vocabulary.js";

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
			<!-- An episode too, wearing its season's art, as the apps draw it. -->
			<CardGrid cards={group.cards} />
		</section>
	{:else}
		<p class="text-ink-3">
			Nothing here yet. Choose the heart on a title to keep it here.
		</p>
	{/each}
</div>
