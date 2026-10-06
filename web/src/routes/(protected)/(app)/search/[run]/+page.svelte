<script lang="ts">
import CardGrid from "#lib/components/CardGrid.svelte";
import PersonCard from "#lib/components/PersonCard.svelte";

let { data } = $props();
</script>

<svelte:head
	><title>{data.name} · {data.q} · Search · Photon</title></svelte:head
>

<div class="grid gap-6">
	<div>
		<a
			href="/search?q={encodeURIComponent(data.q)}"
			class="label hover:underline"
		>
			Results for “{data.q}”
		</a>
		<h1 class="title">{data.name}</h1>
	</div>
	{#if data.people}
		<CardGrid items={data.people}>
			{#snippet card(
				person: NonNullable<typeof data.people>[number],
			)}
				<PersonCard {...person} />
			{/snippet}
		</CardGrid>
	{:else if data.cards}
		<CardGrid
			cards={data.cards}
			shape={data.kind === "episode" ? "still" : "poster"}
		/>
	{/if}
</div>
