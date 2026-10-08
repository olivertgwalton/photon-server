<script lang="ts">
import { page } from "$app/state";
import { withQuery } from "#lib/address.js";
import { Button } from "#lib/components/ui/button/index.js";

// Newer and older pages of a list the server pages by offset, as links, so
// each page has an address.
let { offset, limit, total }: { offset: number; limit: number; total: number } =
	$props();

const at = (next: number) =>
	withQuery(page.url, { offset: next ? String(next) : undefined });
</script>

{#if total > limit}
	<nav aria-label="Pages" class="flex items-center justify-between gap-4">
		{#if offset > 0}
			<Button href={at(Math.max(offset - limit, 0))} variant="outline"
				>Newer</Button
			>
		{:else}
			<span></span>
		{/if}
		<p class="text-ink-3 text-sm">
			{offset + 1}–{Math.min(offset + limit, total)}
			of {total}
		</p>
		{#if offset + limit < total}
			<Button href={at(offset + limit)} variant="outline">Older</Button>
		{:else}
			<span></span>
		{/if}
	</nav>
{/if}
