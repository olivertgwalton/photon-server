<script lang="ts">
import HeartIcon from "@lucide/svelte/icons/heart";
import { setMark } from "#lib/actions.ts";
import type { components } from "#lib/api/schema.d.ts";
import { seriesLine } from "#lib/books.ts";
import CardGrid from "#lib/components/CardGrid.svelte";
import CoverHero from "#lib/components/CoverHero.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import { count } from "#lib/format.ts";

type Schemas = components["schemas"];

// An author: their picture, and their books, a series' together in its
// order, as Audiobookshelf's author page has them.
let { title: t }: { title: Schemas["TitlePage"] } = $props();

const books = $derived(
	(t.books ?? []).toSorted(
		(a, b) =>
			(a.series ?? "").localeCompare(b.series ?? "") ||
			(a.series_index ?? 0) - (b.series_index ?? 0) ||
			(a.year ?? 0) - (b.year ?? 0),
	),
);
const favourite = $derived(!!t.state?.favourite_at);
</script>

<article class="grid gap-10">
	<CoverHero
		title={t}
		shape="poster"
		facts={[books.length > 0 && count(books.length, "book")]}
	>
		{#snippet actions()}
			<Button
				variant="glass"
				size="icon-lg"
				aria-pressed={favourite}
				aria-label="Favourite"
				title={favourite ? "Remove from favourites" : "Add to favourites"}
				onclick={() => setMark(t.id, "favourite", !favourite)}
			>
				<HeartIcon class={favourite ? "fill-current" : "text-ink-3"} />
			</Button>
		{/snippet}
	</CoverHero>

	{#if books.length}
		<section aria-labelledby="books">
			<h2 id="books" class="heading mb-3">Books</h2>
			<CardGrid
				cards={books}
				caption={(i) =>
					seriesLine(books[i].series, books[i].series_index) ??
					(books[i].year ? String(books[i].year) : "")}
			/>
		</section>
	{/if}
</article>
