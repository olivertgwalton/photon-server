<script lang="ts">
import type { components } from "#lib/api/schema.js";
import { blurStyle } from "#lib/blurhash.js";
import Artwork from "#lib/components/Artwork.svelte";
import Rail from "#lib/components/Rail.svelte";

type Card = components["schemas"]["Card"];

// The titles a search found that the server does not hold, which its owner
// lets people ask for. Each opens as a title's page does, with a way to ask for
// it in Play's place; none plays, and none has a title's menu.
let { cards }: { cards: Card[] } = $props();
</script>

<Rail title="Not in library" items={cards}>
	{#snippet card(
		c: Card,
		sizes: string,
	)}
		<a href="/titles/{c.id}" class="group block outline-none">
			<div
				class="card-frame aspect-2/3"
				style={c.poster ? blurStyle(c.blurhashes?.[c.poster]) : undefined}
			>
				{#if c.poster}
					<Artwork id={c.poster} shape="poster" {sizes} class="card-picture" />
				{/if}
				<div class="card-shade grid gap-0.5">
					<p
						class="text-ink line-clamp-2 text-[0.8125rem] leading-tight font-semibold"
					>
						{c.title}
					</p>
					{#if c.year}
						<p class="text-ink-2 text-xs">{c.year}</p>
					{/if}
				</div>
			</div>
		</a>
	{/snippet}
</Rail>
