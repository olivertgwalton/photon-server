<script lang="ts">
import { artworkSrc, artworkSrcset } from "#lib/artwork.js";
import type { components } from "#lib/api/schema.js";
import CardGrid from "#lib/components/CardGrid.svelte";
import { fold } from "#lib/credits.js";
import { wallSearch } from "#lib/wall.js";

type Credit = components["schemas"]["Credit"];

let { data } = $props();

const p = $derived(data.person);

// Their work here, one card a title whatever they did on it, under the first
// of these they did, acting first.
const groups: [string, Credit["credit"][]][] = [
	["Acting", ["actor", "guest_star"]],
	["Directing", ["director"]],
	["Creating", ["creator"]],
	["Writing", ["writer"]],
	["Producing", ["producer"]],
	["Music", ["composer"]],
];
const titles = $derived(
	fold(
		p.credits,
		(c) => c.id,
		(c) => c.credit,
		(c) => c.role,
	),
);
const work = $derived(
	groups
		.map(([name, kinds]) => ({
			name,
			titles: titles.filter(
				(f) =>
					groups.find(([, ks]) => f.kinds.some((k) => ks.includes(k)))?.[1] ===
					kinds,
			),
		}))
		.filter((g) => g.titles.length),
);

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

	{#each work as group (group.name)}
		<section aria-labelledby="work-{group.name}">
			<h2 id="work-{group.name}" class="heading mb-3">{group.name}</h2>
			<CardGrid
				cards={group.titles.map((f) => f.all[0])}
				caption={(i: number) => {
					const f = group.titles[i];
					return [f.all[0].year, f.said].filter(Boolean).join(" · ");
				}}
			/>
		</section>
	{/each}
</article>
