<script lang="ts">
import { client } from "#lib/api/client.js";
import Wall from "#lib/components/Wall.svelte";
import { homeRows } from "#lib/rows.js";
import { wallPageSize } from "#lib/wall.js";

let { data } = $props();

const row = $derived(homeRows[data.kind]);

async function fetchPage(offset: number) {
	const { data: page } = await client().GET("/api/v1/home/{row}", {
		params: {
			path: { row: data.kind },
			query: { offset, limit: wallPageSize },
		},
	});
	return page?.items;
}
</script>

<svelte:head><title>{row.title} · Photon</title></svelte:head>

<h1 class="title mb-6">{row.title}</h1>
{#if data.page.total}
	<Wall
		total={data.page.total}
		first={data.page.items}
		pageSize={wallPageSize}
		{fetchPage}
		view={row.shape}
		label={row.title}
	/>
{:else}
	<p class="text-ink-3">Nothing here now.</p>
{/if}
