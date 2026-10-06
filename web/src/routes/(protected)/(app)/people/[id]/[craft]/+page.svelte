<script lang="ts">
import CardGrid from "#lib/components/CardGrid.svelte";

let { data } = $props();

const p = $derived(data.person);
const group = $derived(data.group);
</script>

<svelte:head><title>{group.name} · {p.name} · Photon</title></svelte:head>

<div class="grid gap-6">
	<div>
		<a href="/people/{p.id}" class="label hover:underline">{p.name}</a>
		<h1 class="title">{group.name}</h1>
	</div>
	<CardGrid cards={group.cards} caption={(i: number) => group.captions[i]} />
</div>
