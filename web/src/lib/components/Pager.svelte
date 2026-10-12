<script lang="ts">
import { page } from "$app/state";
import { withQuery } from "#lib/address.js";
import { Button } from "#lib/components/ui/button/index.js";
import { pageSize } from "#lib/paging.js";

// Newer and older pages of a list the server pages by offset, as links, so
// each page has an address.
let { offset, total }: { offset: number; total: number } = $props();

const at = (next: number) =>
	withQuery(page.url, { offset: next ? String(next) : undefined });
</script>

{#if total > pageSize}
	<nav aria-label="Pages" class="flex items-center justify-between gap-4">
		{#if offset > 0}
			<Button href={at(Math.max(offset - pageSize, 0))} variant="outline"
				>Newer</Button
			>
		{:else}
			<span></span>
		{/if}
		<p class="text-ink-3 text-sm">
			{offset + 1}–{Math.min(offset + pageSize, total)}
			of {total}
		</p>
		{#if offset + pageSize < total}
			<Button href={at(offset + pageSize)} variant="outline">Older</Button>
		{:else}
			<span></span>
		{/if}
	</nav>
{/if}
