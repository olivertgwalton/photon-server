<script lang="ts">
import { client } from "#lib/api/client.js";
import Wall from "#lib/components/Wall.svelte";
import { wallPageSize } from "#lib/wall.js";

let { data } = $props();

async function fetchPage(offset: number) {
	const { data: page } = await client().GET(
		"/api/v1/libraries/{id}/collections",
		{
			params: {
				path: { id: data.library.id },
				query: { offset, limit: wallPageSize },
			},
		},
	);
	return page?.items;
}
</script>

<svelte:head>
	<title>Collections · {data.library.name} · Photon</title>
</svelte:head>

<h2 class="sr-only">Collections</h2>
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
	<p class="text-ink-3">No collections in {data.library.name} yet.</p>
{/if}
