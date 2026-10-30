<script lang="ts" module>
// The title the reader last rested on or went to from Home, which the curated
// showcase wears, kept while they are away from Home. By id, so it is drawn as
// Home last answered it; one no longer on Home leaves the showcase its pool.
const resting = $state<{ id?: string }>({});
</script>

<script lang="ts">
import { refusal } from "#lib/api/client.js";
import Rail from "#lib/components/Rail.svelte";
import Showcase from "#lib/components/Showcase.svelte";
import { homeRows } from "#lib/rows.js";
import { readShowcase, showcasePool } from "#lib/showcase.js";
import { vocabulary } from "#lib/vocabulary.js";

let { data } = $props();
const words = vocabulary();

const showcase = readShowcase();
const pool = $derived(
	showcase.visibility === "shown"
		? showcasePool(data.rails, showcase.row)
		: undefined,
);
// Every title on Home once each row has answered, for the one the reader
// rested on: Home asked again is read afresh, so one gone from it is not kept.
const known = $derived(
	Promise.allSettled(data.rails.map((r) => r.cards)).then(
		(answers) =>
			new Map(
				answers.flatMap((a) =>
					a.status === "fulfilled"
						? a.value.items.map((c) => [c.id, c] as const)
						: [],
				),
			),
	),
);

// What the page says in place of its rows once every row has answered: why
// none could be read, or that none has anything.
const instead = $derived(
	Promise.allSettled(data.rails.map((r) => r.cards)).then((answers) => {
		const refused = answers.find((a) => a.status === "rejected");
		if (refused && answers.every((a) => a.status === "rejected"))
			return { refused: refusal(refused.reason) };
		const any = answers.some(
			(a) => a.status === "fulfilled" && a.value.items.length,
		);
		return any ? undefined : { refused: undefined };
	}),
);

const unread = { items: [] };

function title(r: (typeof data.rails)[number]) {
	return r.library
		? words.library_rows[r.kind]?.replace("{library}", r.library.name)
		: words.rows[r.kind];
}
</script>

<svelte:head><title>Home · Photon</title></svelte:head>

<h1 class="sr-only">Home</h1>
<!-- Each row is drawn as it comes, and kept while a change the server told of
	is asked for again; a row that could not be read is left out. -->
<div class="grid gap-8">
	{#if pool}
		<svelte:boundary>
			{@const { cards, turn } = await pool}
			{@const rested =
				turn === "rests" && resting.id
					? (await known).get(resting.id)
					: undefined}
			{#if cards.length}
				<!-- Curated, the showcase wears the title the reader rests on. -->
				<Showcase cards={turn === "slides" ? cards : [rested ?? cards[0]]} />
			{/if}
			{#snippet pending()}
				<!-- Its room is kept, so the rows drawn under it do not move. -->
				<div class="-mx-3 -mt-6 min-h-[max(30rem,62svh)] sm:-mx-6"></div>
			{/snippet}
		</svelte:boundary>
	{/if}
	{#each data.rails as r (r.key)}
		<svelte:boundary>
			{@const page = await r.cards.catch(() => unread)}
			{#if page.items.length}
				<Rail
					title={title(r) ?? r.kind}
					cards={page.items}
					shape={homeRows[r.kind].shape}
					href={r.href}
					pill="always"
					resume={r.kind === "continue_watching" ? "shown" : "hidden"}
					onrest={(card) => {
						resting.id = card.id;
					}}
				/>
			{/if}
			{#snippet pending()}
				<!-- Nothing, until it answers. -->
			{/snippet}
		</svelte:boundary>
	{/each}
</div>

<svelte:boundary>
	{@const said = await instead}
	{#if said?.refused}
		<div class="grid min-h-[50svh] place-content-center gap-2 text-center">
			<p class="heading">{said.refused}</p>
		</div>
	{:else if said}
		<div class="grid min-h-[50svh] place-content-center gap-2 text-center">
			<p class="heading">Nothing to watch yet</p>
			<p class="text-sm">Titles appear here once a library has been scanned.</p>
		</div>
	{/if}
	{#snippet pending()}
		<!-- Nothing, until every row has answered. -->
	{/snippet}
</svelte:boundary>
