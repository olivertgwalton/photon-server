<script lang="ts">
import CardGrid from "#lib/components/CardGrid.svelte";
import ExtraCard from "#lib/components/ExtraCard.svelte";
import PersonCard from "#lib/components/PersonCard.svelte";
import type { Extra } from "#lib/extras.js";
import { count } from "#lib/format.js";

let { data } = $props();

const t = $derived(data.title);
</script>

<svelte:head><title>{data.name} · {t.title} · Photon</title></svelte:head>

<div class="grid gap-6">
	<div>
		<a href="/titles/{t.id}" class="label hover:underline">{t.title}</a>
		<h1 class="title">{data.name}</h1>
	</div>
	{#if data.run === "seasons"}
		<CardGrid
			cards={data.seasons}
			caption={(i: number) => count(data.seasons[i].episodes, "episode")}
		/>
	{:else if data.run === "cast"}
		<CardGrid items={data.cast}>
			{#snippet card(
				credit: (typeof data.cast)[number],
			)}
				<PersonCard
					id={credit.person_id}
					name={credit.name}
					photo={credit.photo}
					blurhashes={credit.blurhashes}
					caption={credit.said}
				/>
			{/snippet}
		</CardGrid>
	{:else if data.run === "extras"}
		<CardGrid items={data.extras} shape="still">
			{#snippet card(
				item: Extra,
			)}
				<ExtraCard {item} sizes="20rem" />
			{/snippet}
		</CardGrid>
	{:else if data.run === "collections"}
		<CardGrid cards={data.collections} />
	{:else}
		<CardGrid cards={data.similar} />
	{/if}
</div>
