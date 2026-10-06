<script lang="ts">
import { goto } from "$app/navigation";
import CardGrid from "#lib/components/CardGrid.svelte";
import PersonCard from "#lib/components/PersonCard.svelte";
import * as Select from "#lib/components/ui/select/index.js";
import { byKind } from "#lib/rows.js";

let { data } = $props();

const groups = $derived(byKind(data.results?.items ?? []));
const nothing = $derived(
	data.results && !data.results.items.length && !data.results.people.length,
);

function scope(library: string) {
	const query = new URLSearchParams({ q: data.q });
	if (library) query.set("library", library);
	goto(`/search?${query}`, { replace: true, reset: false });
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
			<Select.Root
				type="single"
				value={data.library ?? ""}
				onValueChange={scope}
			>
				<Select.Trigger aria-label="Search in" class="min-w-44">
					{data.libraries.find((l) => l.id === data.library)?.name ??
						"Every library"}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="" label="Every library" />
					{#each data.libraries as library (library.id)}
						<Select.Item value={library.id} label={library.name} />
					{/each}
				</Select.Content>
			</Select.Root>
		{/if}
	</div>

	{#if !data.q}
		<p class="text-ink-3">Search for a film, a show, an episode or a person.</p>
	{:else if nothing}
		<p class="text-ink-3" role="status">Nothing here matches “{data.q}”.</p>
	{/if}

	{#each groups as group (group.name)}
		<section aria-labelledby="found-{group.name}">
			<h2 id="found-{group.name}" class="heading mb-3">{group.name}</h2>
			<CardGrid
				cards={group.cards}
				shape={group.kind === "episode" ? "still" : "poster"}
			/>
		</section>
	{/each}

	{#if data.results?.people.length}
		<section aria-labelledby="found-people">
			<h2 id="found-people" class="heading mb-3">People</h2>
			<ul class="grid grid-cols-[repeat(auto-fill,minmax(8rem,1fr))] gap-4">
				{#each data.results.people as person (person.id)}
					<li>
						<PersonCard {...person} />
					</li>
				{/each}
			</ul>
		</section>
	{/if}
</div>
