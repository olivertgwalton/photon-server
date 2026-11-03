<script lang="ts">
import type { Snippet } from "svelte";
import type { components } from "#lib/api/schema.d.ts";
import { aspect } from "#lib/artwork.ts";
import { blurStyle, tintColour } from "#lib/blurhash.ts";
import Artwork from "#lib/components/Artwork.svelte";
import Prose from "#lib/components/Prose.svelte";

// The head of a page about music or a book: its cover beside its name, who
// it is by, and what can be done with it, over its backdrop where it has one,
// as Plex's album and audiobook pages have it.
let {
	title: t,
	shape,
	by,
	facts = [],
	actions,
}: {
	title: components["schemas"]["TitlePage"];
	shape: "square" | "poster";
	by?: { id: string; title: string } | null;
	facts?: (string | number | false | undefined)[];
	actions?: Snippet;
} = $props();

const backdrop = $derived(t.artwork?.backdrop?.[0]);
const cover = $derived(t.artwork?.poster?.[0]);
const accent = $derived(
	(backdrop && tintColour(t.blurhashes?.[backdrop])) ?? "transparent",
);
const shown = $derived(facts.filter((f) => f !== false && f !== undefined));
</script>

<header class="relative -mx-3 -mt-6 min-w-0 sm:-mx-6" style:--accent={accent}>
	{#if backdrop}
		<div
			class="absolute inset-x-0 top-0 h-[60svh] overflow-hidden"
			style={blurStyle(t.blurhashes?.[backdrop])}
		>
			{#key backdrop}
				<Artwork
					id={backdrop}
					shape="backdrop"
					loading="eager"
					sizes="100vw"
					fetchpriority="high"
					class="size-full object-cover object-top transition-opacity duration-700 data-loading:opacity-0"
				/>
			{/key}
			<div
				class="from-ground via-(--accent)/35 absolute inset-0 bg-linear-to-t to-transparent"
			></div>
		</div>
	{/if}
	<div
		class={[
			"relative flex gap-6 px-3 max-sm:flex-col max-sm:items-center max-sm:text-center sm:items-end sm:px-6",
			backdrop ? "pt-[22svh]" : "pt-6",
		]}
	>
		<div
			class={[
				"bg-raise w-48 shrink-0 overflow-hidden rounded-lg shadow-2xl sm:w-56",
				aspect[shape].class,
			]}
			style={cover ? blurStyle(t.blurhashes?.[cover]) : undefined}
		>
			{#if cover}
				{#key cover}
					<Artwork
						id={cover}
						{shape}
						loading="eager"
						sizes="14rem"
						class="size-full object-cover"
					/>
				{/key}
			{/if}
		</div>
		<div
			class="grid min-w-0 gap-3 max-sm:justify-items-center sm:justify-items-start"
		>
			{#if by}
				<a href="/titles/{by.id}" class="text-ink-2 text-sm hover:underline">
					{by.title}
				</a>
			{/if}
			<h1 class="title max-w-3xl text-3xl md:text-5xl">{t.title}</h1>
			{#if shown.length}
				<p
					class="text-ink-2 flex flex-wrap gap-x-3 gap-y-1 text-sm max-sm:justify-center"
				>
					{#each shown as fact (fact)}
						<span>{fact}</span>
					{/each}
				</p>
			{/if}
			{#if t.genres?.length}
				<p class="text-ink-3 text-sm">{t.genres.join(", ")}</p>
			{/if}
			{#if actions}
				<div class="flex flex-wrap items-center gap-2 max-sm:justify-center">
					{@render actions()}
				</div>
			{/if}
		</div>
	</div>
	{#if t.overview}
		<div class="glass-plate relative mx-3 mt-6 max-w-3xl sm:mx-6">
			<Prose text={t.overview} lines={3} title={t.title} class="text-ink-2" />
		</div>
	{/if}
</header>
