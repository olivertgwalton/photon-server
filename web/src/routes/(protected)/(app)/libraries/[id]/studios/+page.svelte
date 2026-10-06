<script lang="ts">
import { wallSearch } from "#lib/wall.js";

let { data } = $props();

const name = $derived(data.library.kind === "shows" ? "Networks" : "Studios");
</script>

<svelte:head><title>{name} · {data.library.name} · Photon</title></svelte:head>

<h2 class="sr-only">{name}</h2>
<ul class="grid grid-cols-[repeat(auto-fill,minmax(14rem,1fr))] gap-2">
	{#each data.facets.studios as studio (studio)}
		<li>
			<a
				href="/libraries/{data.library.id}{wallSearch({ studio: [studio] })}"
				class="hover:bg-accent text-ink block truncate rounded-md px-3 py-2"
			>
				{studio}
			</a>
		</li>
	{/each}
</ul>
