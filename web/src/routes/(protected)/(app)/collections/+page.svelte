<script lang="ts">
import { client } from "#lib/api/client.js";
import Wall from "#lib/components/Wall.svelte";
import { wallPageSize } from "#lib/wall.js";

let { data } = $props();

async function fetchPage(offset: number) {
	const { data: page } = await client().GET("/api/v1/collections", {
		params: { query: { offset, limit: wallPageSize } },
	});
	return page?.items;
}
</script>

<svelte:head><title>Collections · Photon</title></svelte:head>

<!-- Every collection, of the titles of any library, as Jellyfin's Collections view. -->
<div class="grid gap-4">
	<h1 class="title">Collections</h1>
	{#if data.collections.total}
		<Wall
			total={data.collections.total}
			first={data.collections.items}
			pageSize={wallPageSize}
			{fetchPage}
			view="poster"
			label="Collections"
		/>
	{:else}
		<p class="text-ink-3">No collections yet.</p>
	{/if}
</div>
