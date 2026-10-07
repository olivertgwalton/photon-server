<script lang="ts" generics="T extends Record<'id', string>">
import type { Snippet } from "svelte";
import type { Shape } from "#lib/artwork.js";
import { gridColumns } from "#lib/grid.js";
import TitleCard, { type CardLike } from "./TitleCard.svelte";

// A short list of titles, all drawn: what a wall is before it needs paging.
// Its `cards` are TitleCards; other `items` are drawn by `card`.
let {
	cards = [],
	items = [],
	card,
	shape = "poster",
	caption,
}: {
	cards?: CardLike[];
	items?: T[];
	card?: Snippet<[T]>;
	shape?: Shape;
	caption?: (index: number) => string | undefined;
} = $props();
</script>

<ul
	class="grid gap-x-3 gap-y-4.5"
	style="grid-template-columns: {gridColumns(shape)}"
>
	{#each cards as c, i (`${c.id}-${i}`)}
		<li>
			<TitleCard
				card={c}
				{shape}
				sizes={shape === "poster" ? "12rem" : "20rem"}
				caption={caption?.(i)}
			/>
		</li>
	{/each}
	{#if card}
		{#each items as item, i (`${item.id}-${i}`)}
			<li>{@render card(item)}</li>
		{/each}
	{/if}
</ul>
