<script lang="ts">
import CheckIcon from "@lucide/svelte/icons/check";
import type { Snippet } from "svelte";
import type { components } from "#lib/api/schema.js";
import { blurStyle } from "#lib/blurhash.js";
import Artwork from "#lib/components/Artwork.svelte";
import { runtime, showLine } from "#lib/format.js";
import TitleMenuButton from "./TitleMenuButton.svelte";

type Card = components["schemas"]["Card"];

// A title as a line in a list: a small poster, its name and what it is, and
// at the end, the menu or whatever the list puts there instead.
let { card, detail, end }: { card: Card; detail?: string; end?: Snippet } =
	$props();

const picture = $derived(card.poster ?? card.thumb);
const line = $derived(
	detail ??
		[
			card.kind === "episode" || card.kind === "season"
				? showLine(card)
				: card.year,
			card.duration_ms && runtime(card.duration_ms),
		]
			.filter(Boolean)
			.join(" · "),
);
</script>

<div class="flex items-center gap-3">
	<a
		href="/titles/{card.id}"
		class="group flex min-w-0 flex-1 items-center gap-3 rounded-lg outline-none"
	>
		<span
			class="bg-raise group-focus-visible:ring-signal group-hover:ring-line-strong block aspect-2/3 w-12 shrink-0 overflow-hidden rounded-md ring-2 ring-transparent"
		>
			{#if picture}
				<Artwork
					id={picture}
					shape="poster"
					sizes="3rem"
					class="size-full object-cover"
					style={blurStyle(card.blurhashes?.[picture])}
				/>
			{/if}
		</span>
		<span class="min-w-0">
			<span class="text-ink block truncate font-semibold group-hover:underline">
				{card.title}
			</span>
			<span class="text-ink-3 block truncate text-sm">{line}</span>
		</span>
	</a>
	{#if card.state?.watched_at}
		<CheckIcon class="text-ink-3 size-4 shrink-0" aria-hidden="true" />
		<span class="sr-only">Watched</span>
	{/if}
	{#if end}
		{@render end()}
	{:else}
		<TitleMenuButton {card} />
	{/if}
</div>
