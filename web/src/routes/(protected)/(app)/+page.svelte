<script lang="ts">
import Rail from "#lib/components/Rail.svelte";
import { homeRows, rail } from "#lib/rows.js";
import { settled } from "#lib/settled.svelte.js";

let { data } = $props();
const home = settled(() => data.home);
</script>

<svelte:head><title>Home · Photon</title></svelte:head>

<h1 class="sr-only">Home</h1>
{#if home.failed}
	<div class="grid min-h-[50svh] place-content-center gap-2 text-center">
		<p class="heading">{home.failed}</p>
	</div>
{:else if home.value?.rows.length}
	<div class="grid gap-8">
		{#each home.value.rows.map((row) => ({
			row,
			...rail(row),
		})) as { row, key, href } (key)}
			<Rail
				title={row.title}
				cards={row.items}
				shape={homeRows[row.kind].shape}
				{href}
			/>
		{/each}
	</div>
{:else if home.value}
	<div class="grid min-h-[50svh] place-content-center gap-2 text-center">
		<p class="heading">Nothing to watch yet</p>
		<p class="text-sm">Titles appear here once a library has been scanned.</p>
	</div>
{/if}
