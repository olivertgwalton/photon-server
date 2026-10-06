<script lang="ts">
import type { Shape } from "#lib/artwork.js";
import type { components } from "#lib/api/schema.js";
import { gridColumns } from "#lib/grid.js";
import TitleCard from "./TitleCard.svelte";

// A short list of titles, all drawn: what a wall is before it needs paging.
let {
	cards,
	shape = "poster",
	caption,
}: {
	cards: components["schemas"]["Card"][];
	shape?: Shape;
	caption?: (index: number) => string | undefined;
} = $props();
</script>

<ul
	class="grid gap-x-4 gap-y-6"
	style="grid-template-columns: {gridColumns(shape)}"
>
	{#each cards as card, i (`${card.id}-${i}`)}
		<li>
			<TitleCard
				{card}
				{shape}
				sizes={shape === "poster" ? "12rem" : "20rem"}
				caption={caption?.(i)}
			/>
		</li>
	{/each}
</ul>
