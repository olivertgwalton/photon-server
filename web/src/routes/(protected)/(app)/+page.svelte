<script lang="ts">
import Rail from "#lib/components/Rail.svelte";
import { homeRows } from "#lib/rows.js";

let { data } = $props();
</script>

<svelte:head><title>Home · Photon</title></svelte:head>

<h1 class="sr-only">Home</h1>
{#if data.home.rows.length}
	<div class="grid gap-8">
		{#each data.home.rows as row (row.kind)}
			<Rail
				title={homeRows[row.kind].title}
				cards={row.items}
				shape={homeRows[row.kind].shape}
				href="/home/{row.kind}"
			/>
		{/each}
	</div>
{:else}
	<div class="grid min-h-[50svh] place-content-center gap-2 text-center">
		<p class="heading">Nothing to watch yet</p>
		<p class="text-sm">Titles appear here once a library has been scanned.</p>
	</div>
{/if}
