<script lang="ts">
import Rail from "#lib/components/Rail.svelte";
import { homeRows } from "#lib/rows.js";
import { settled } from "#lib/settled.svelte.js";
import { vocabulary } from "#lib/vocabulary.js";

let { data } = $props();
const words = vocabulary();

// Every row's answer, to say once they are all in whether the page has
// nothing, or could get nothing.
const all = settled(() =>
	Promise.allSettled(data.rails.map((r) => r.cards)).then((answers) => {
		const refused = answers.find((a) => a.status === "rejected");
		if (answers.length && answers.every((a) => a.status === "rejected"))
			throw refused?.reason;
		return answers.some(
			(a) => a.status === "fulfilled" && a.value.items.length,
		);
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
{#if all.failed}
	<div class="grid min-h-[50svh] place-content-center gap-2 text-center">
		<p class="heading">{all.failed}</p>
	</div>
{:else if all.value === false}
	<div class="grid min-h-[50svh] place-content-center gap-2 text-center">
		<p class="heading">Nothing to watch yet</p>
		<p class="text-sm">Titles appear here once a library has been scanned.</p>
	</div>
{:else}
	<div class="grid gap-8">
		{#each data.rails as r (r.key)}
			<!-- A row that could not be read is left out; the rest are drawn. -->
			{#await r.cards.catch(() => unread) then page}
				{#if page.items.length}
					<Rail
						title={title(r) ?? r.kind}
						cards={page.items}
						shape={homeRows[r.kind].shape}
						href={r.href}
						pill="always"
					/>
				{/if}
			{/await}
		{/each}
	</div>
{/if}
