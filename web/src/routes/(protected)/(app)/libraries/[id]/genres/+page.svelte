<script lang="ts">
import { wallSearch } from "#lib/wall.js";

let { data } = $props();
</script>

<svelte:head><title>Genres · {data.library.name} · Photon</title></svelte:head>

<h2 class="sr-only">Genres</h2>
<ul class="grid grid-cols-[repeat(auto-fill,minmax(12rem,1fr))] gap-3">
	{#each data.facets.genres as genre (genre)}
		<li>
			<a
				href="/libraries/{data.library.id}{wallSearch({ genre: [genre] })}"
				class="bg-raise hover:bg-accent text-ink block rounded-lg px-4 py-6 font-heading text-lg font-bold"
			>
				{genre}
			</a>
		</li>
	{/each}
</ul>
