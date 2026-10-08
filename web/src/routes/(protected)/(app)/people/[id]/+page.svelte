<script lang="ts">
import Artwork from "#lib/components/Artwork.svelte";
import { blurStyle } from "#lib/blurhash.js";
import Prose from "#lib/components/Prose.svelte";
import Rail from "#lib/components/Rail.svelte";
import { wallSearch } from "#lib/wall.js";

let { data } = $props();

const p = $derived(data.person);

const date = (d: string) =>
	new Date(d).toLocaleDateString(undefined, {
		dateStyle: "long",
		timeZone: "UTC",
	});
const life = $derived(
	[
		p.born &&
			`Born ${date(p.born)}${p.birthplace ? ` in ${p.birthplace}` : ""}`,
		p.died && `Died ${date(p.died)}`,
	].filter(Boolean),
);
</script>

<svelte:head><title>{p.name} · Photon</title></svelte:head>

<article class="grid gap-10">
	<header class="flex flex-col gap-6 sm:flex-row sm:gap-8">
		{#if p.photo}
			<Artwork
				id={p.photo}
				shape="poster"
				loading="eager"
				sizes="200px"
				class="aspect-[2/3] w-40 shrink-0 self-start rounded-xl object-cover shadow-2xl sm:w-50"
				style={blurStyle(p.blurhashes?.[p.photo])}
			/>
		{/if}
		<div class="grid content-start gap-3">
			<h1 class="title">{p.name}</h1>
			{#each life as line, i (i)}
				<p class="text-ink-2 text-sm">{line}</p>
			{/each}
			{#if p.biography}
				<Prose
					text={p.biography}
					lines={4}
					title={p.name}
					class="text-ink-2 max-w-2xl"
				/>
			{/if}
			{#if data.libraries.length}
				<p class="flex flex-wrap gap-x-4 gap-y-1 text-sm">
					{#each data.libraries as library (library.id)}
						<a
							href="/libraries/{library.id}{wallSearch({ person: [p.id] })}"
							class="text-ink hover:underline"
						>
							Browse {library.name}
						</a>
					{/each}
				</p>
			{/if}
		</div>
	</header>

	{#each data.work as group (group.slug)}
		<Rail
			title={group.name}
			cards={group.cards}
			caption={(i: number) => group.captions[i]}
			href="/people/{p.id}/{group.slug}"
		/>
	{/each}
</article>
