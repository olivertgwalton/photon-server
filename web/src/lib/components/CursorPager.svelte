<script lang="ts">
import { withQuery } from "#lib/address.js";
import { Button } from "#lib/components/ui/button/index.js";
import { page } from "$app/state";

// The newest and the next older page of a list the server pages on from the
// page before, as links, so each page has an address.
let { next }: { next?: string } = $props();

const paged = $derived(page.url.searchParams.has("after"));
</script>

{#if paged || next}
	<nav aria-label="Pages" class="flex items-center justify-between gap-4">
		{#if paged}
			<Button href={withQuery(page.url, { after: undefined })} variant="outline"
				>Newest</Button
			>
		{:else}
			<span></span>
		{/if}
		{#if next}
			<Button href={withQuery(page.url, { after: next })} variant="outline"
				>Older</Button
			>
		{:else}
			<span></span>
		{/if}
	</nav>
{/if}
