<script lang="ts">
import CardGrid from "#lib/components/CardGrid.svelte";
import { vocabulary } from "#lib/vocabulary.ts";

// What one of a library's rails holds, as the Photon apps draw it: its own page,
// not the library's, so it carries none of the library's tabs.
let { data } = $props();

const words = vocabulary();
const title = $derived(
	words.library_rows[data.kind]?.replace("{library}", data.library.name),
);
</script>

<svelte:head><title>{title} · Photon</title></svelte:head>

<div class="grid gap-8">
	<h1 class="title">{title}</h1>
	{#if data.items.length}
		<CardGrid cards={data.items} />
	{:else}
		<p class="text-ink-3">Nothing here now.</p>
	{/if}
</div>
