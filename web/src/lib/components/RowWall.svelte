<script lang="ts">
import { client } from "#lib/api/client.ts";
import type { components } from "#lib/api/schema.d.ts";
import Wall from "#lib/components/Wall.svelte";
import { homeRows } from "#lib/rows.ts";
import { vocabulary } from "#lib/vocabulary.ts";
import { wallPageSize } from "#lib/wall.ts";

type Kind = components["schemas"]["HomeRowKind"];

// The whole of one home row, a page at a time, on posters as the apps draw it:
// even Continue Watching, whose rail is of stills.
let { kind, page }: { kind: Kind; page: components["schemas"]["CardPage"] } =
	$props();

const words = vocabulary();
const row = $derived(homeRows[kind]);
const title = $derived(words.rows[kind]);

async function fetchPage(offset: number) {
	const { data } = await client().GET("/api/v1/profile/rows/{row}", {
		params: { path: { row: kind }, query: { offset, limit: wallPageSize } },
	});
	return data?.items;
}
</script>

<svelte:head><title>{title} · Photon</title></svelte:head>

<div class="grid gap-8">
	<h1 class="title">{title}</h1>
	{#if page.total}
		<Wall
			total={page.total}
			first={page.items}
			pageSize={wallPageSize}
			{fetchPage}
			view="poster"
			label={title}
		/>
	{:else}
		<p class="text-ink-3">{row.empty ?? "Nothing here now."}</p>
	{/if}
</div>
