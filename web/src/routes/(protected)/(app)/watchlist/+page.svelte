<script lang="ts">
import { client } from "#lib/api/client.js";
import Wall from "#lib/components/Wall.svelte";
import { wallPageSize } from "#lib/wall.js";

let { data } = $props();

async function fetchPage(offset: number) {
	const { data: page } = await client().GET("/api/v1/home/{row}", {
		params: {
			path: { row: "watchlist" },
			query: { offset, limit: wallPageSize },
		},
	});
	return page?.items;
}
</script>

<svelte:head><title>Watchlist · Photon</title></svelte:head>

<h1 class="title mb-6">Watchlist</h1>
{#if data.watchlist.total}
	<Wall
		total={data.watchlist.total}
		first={data.watchlist.items}
		pageSize={wallPageSize}
		{fetchPage}
		view="poster"
		label="Watchlist"
	/>
{:else}
	<p class="text-ink-3">
		Nothing here yet. Choose the bookmark on a film or show to keep it here
		until it is watched.
	</p>
{/if}
