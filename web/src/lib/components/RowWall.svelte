<script lang="ts">
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Wall from "#lib/components/Wall.svelte";
import { homeRows } from "#lib/rows.js";
import { wallPageSize } from "#lib/wall.js";

type Kind = components["schemas"]["HomeRowKind"];

// The whole of one home row, a page at a time.
let { kind, page }: { kind: Kind; page: components["schemas"]["CardPage"] } =
	$props();

const row = $derived(homeRows[kind]);

async function fetchPage(offset: number) {
	const { data } = await client().GET("/api/v1/home/{row}", {
		params: { path: { row: kind }, query: { offset, limit: wallPageSize } },
	});
	return data?.items;
}
</script>

<svelte:head><title>{row.title} · Photon</title></svelte:head>

<h1 class="title mb-6">{row.title}</h1>
{#if page.total}
	<Wall
		total={page.total}
		first={page.items}
		pageSize={wallPageSize}
		{fetchPage}
		view={row.shape}
		label={row.title}
	/>
{:else}
	<p class="text-ink-3">{row.empty ?? "Nothing here now."}</p>
{/if}
