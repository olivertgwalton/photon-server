<script lang="ts">
import { vocabulary } from "#lib/vocabulary.js";
import { goto } from "$app/navigation";
import { page } from "$app/state";
import { withQuery } from "#lib/address.js";
import Rail from "#lib/components/Rail.svelte";
import PersonCard from "#lib/components/PersonCard.svelte";
import Choice from "#lib/components/Choice.svelte";
import { byKind } from "#lib/rows.js";

let { data } = $props();
const words = vocabulary();

const groups = $derived(byKind(data.results?.items ?? [], words.kinds));
const nothing = $derived(
	data.results && !data.results.items.length && !data.results.people.length,
);

function scope(library: string) {
	goto(withQuery(page.url, { library }), { replace: true, reset: false });
}
</script>

<svelte:head>
	<title>{data.q ? `${data.q} · Search` : "Search"} · Photon</title>
</svelte:head>

<div class="grid gap-8">
	<div class="flex flex-wrap items-end justify-between gap-4">
		<h1 class="title">
			{#if data.q}
				Results for “{data.q}”
			{:else}
				Search
			{/if}
		</h1>
		{#if data.q && data.libraries.length > 1}
			<Choice
				aria-label="Search in"
				class="min-w-44"
				value={data.library ?? ""}
				options={[
					{ value: "", label: "Every library" },
					...data.libraries.map((l) => ({ value: l.id, label: l.name })),
				]}
				onchange={scope}
			/>
		{/if}
	</div>

	{#if !data.q}
		<p class="text-ink-3">Search for a film, a show, an episode or a person.</p>
	{:else if nothing}
		<p class="text-ink-3" role="status">Nothing here matches “{data.q}”.</p>
	{/if}

	{#each groups as group (group.kind)}
		<Rail
			title={group.name}
			cards={group.cards}
			shape={group.kind === "episode" ? "still" : "poster"}
			href="/search/{group.kind}{page.url.search}"
		/>
	{/each}

	{#if data.results?.people.length}
		<Rail
			title="People"
			items={data.results.people}
			total={data.results.people_total}
			href="/search/people{page.url.search}"
		>
			{#snippet card(
				person: (typeof data.results.people)[number],
			)}
				<PersonCard {...person} />
			{/snippet}
		</Rail>
	{/if}
</div>
