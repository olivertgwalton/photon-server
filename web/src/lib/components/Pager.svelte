<script lang="ts">
import { page } from "$app/state";
import { Button } from "#lib/components/ui/button/index.js";

// Newer and older pages of a list the server pages by offset, as links, so
// each page has an address.
let { offset, limit, total }: { offset: number; limit: number; total: number } =
	$props();

function at(next: number) {
	const to = new URL(page.url.href);
	if (next) to.searchParams.set("offset", String(next));
	else to.searchParams.delete("offset");
	return to.pathname + to.search;
}
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
