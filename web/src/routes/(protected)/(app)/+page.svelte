<script lang="ts">
import type { Shape } from "#lib/artwork.js";
import type { components } from "#lib/api/schema.js";
import Rail from "#lib/components/Rail.svelte";

let { data } = $props();

type Kind = components["schemas"]["HomeRowKind"];

const rows: Record<Kind, { title: string; shape: Shape }> = {
	continue_watching: { title: "Continue Watching", shape: "still" },
	next_up: { title: "Next Up", shape: "still" },
	favourites: { title: "Favourites", shape: "poster" },
	recently_added_films: { title: "Recently Added Films", shape: "poster" },
	recently_added_shows: { title: "Recently Added Shows", shape: "poster" },
};
</script>

<svelte:head><title>Home · Photon</title></svelte:head>

<h1 class="sr-only">Home</h1>
{#if data.home.rows.length}
	<div class="grid gap-8">
		{#each data.home.rows as row (row.kind)}
			<Rail
				title={rows[row.kind].title}
				cards={row.items}
				shape={rows[row.kind].shape}
			/>
		{/each}
	</div>
{:else}
	<div class="grid min-h-[50svh] place-content-center gap-2 text-center">
		<p class="heading">Nothing to watch yet</p>
		<p class="text-sm">Titles appear here once a library has been scanned.</p>
	</div>
{/if}
