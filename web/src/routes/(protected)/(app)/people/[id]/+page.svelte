<script lang="ts">
import { artworkSrc, artworkSrcset } from "#lib/artwork.js";
import Rail from "#lib/components/Rail.svelte";
import { workOf } from "#lib/credits.js";
import { wallSearch } from "#lib/wall.js";

let { data } = $props();

const p = $derived(data.person);

const work = $derived(workOf(p.credits));

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
	<header class="flex flex-col gap-6 sm:flex-row">
		{#if p.photo}
			<img
				src={artworkSrc(p.photo, "poster")}
				srcset={artworkSrcset(p.photo, "poster")}
				sizes="12rem"
				alt=""
				class="aspect-[2/3] w-40 shrink-0 rounded-lg object-cover sm:w-48"
			>
		{/if}
		<div class="grid content-start gap-3">
			<h1 class="title">{p.name}</h1>
			{#each life as line, i (i)}
				<p class="text-ink-2 text-sm">{line}</p>
			{/each}
			{#if p.biography}
				<p class="text-ink-2 max-w-3xl leading-relaxed whitespace-pre-line">
					{p.biography}
				</p>
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

	{#each work as group (group.slug)}
		<Rail
			title={group.name}
			cards={group.cards}
			caption={(i: number) => group.captions[i]}
			href="/people/{p.id}/{group.slug}"
		/>
	{/each}
</article>
