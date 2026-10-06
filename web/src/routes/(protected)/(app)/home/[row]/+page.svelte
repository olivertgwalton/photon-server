<script lang="ts">
import CardGrid from "#lib/components/CardGrid.svelte";
import { homeRows } from "#lib/rows.js";

let { data } = $props();

const row = $derived(homeRows[data.kind]);
</script>

<svelte:head><title>{row.title} · Photon</title></svelte:head>

<h1 class="title mb-6">{row.title}</h1>
{#if data.cards.length}
	<CardGrid cards={data.cards} shape={row.shape} />
{:else}
	<p class="text-ink-3">Nothing here now.</p>
{/if}
