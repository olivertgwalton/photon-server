<script lang="ts">
import type { components } from "#lib/api/schema.d.ts";

// A request's title, a link once the library holds it, and its year.
let {
	request: r,
}: {
	request: Pick<
		components["schemas"]["Request"],
		"title" | "title_id" | "year"
	>;
} = $props();
</script>

{#if r.title_id}
	<a href="/titles/{r.title_id}" class="hover:underline">{r.title}</a>
{:else}
	{r.title}
{/if}
{#if r.year}
	<span class="text-ink-3 font-normal"> ({r.year})</span>
{/if}
