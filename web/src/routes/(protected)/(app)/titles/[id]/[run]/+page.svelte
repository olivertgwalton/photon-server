<script lang="ts">
import { castOf } from "#lib/credits.js";
import CardGrid from "#lib/components/CardGrid.svelte";
import ExtraCard from "#lib/components/ExtraCard.svelte";
import PersonCard from "#lib/components/PersonCard.svelte";
import { type Extra, extrasOf } from "#lib/extras.js";

let { data } = $props();

const t = $derived(data.title);
const cast = $derived(castOf(t.credits ?? []));
const extras: Extra[] = $derived(extrasOf(t));
const collections = $derived(
	(t.collections ?? []).map((c) => ({ ...c, kind: "collection" as const })),
);
</script>

<svelte:head><title>{data.name} · {t.title} · Photon</title></svelte:head>

<div class="grid gap-6">
	<div>
		<a href="/titles/{t.id}" class="label hover:underline">{t.title}</a>
		<h1 class="title">{data.name}</h1>
	</div>
	{#if data.run === "cast"}
		<CardGrid items={cast}>
			{#snippet card(
				credit: (typeof cast)[number],
			)}
				<PersonCard
					id={credit.person_id}
					name={credit.name}
					photo={credit.photo}
					caption={credit.said}
				/>
			{/snippet}
		</CardGrid>
	{:else if data.run === "extras"}
		<CardGrid items={extras} shape="still">
			{#snippet card(
				item: Extra,
			)}
				<ExtraCard {item} />
			{/snippet}
		</CardGrid>
	{:else if data.run === "collections"}
		<CardGrid cards={collections} />
	{:else}
		<CardGrid cards={data.similar} />
	{/if}
</div>
