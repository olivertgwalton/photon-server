<script lang="ts">
import Artwork from "#lib/components/Artwork.svelte";
import { blurStyle } from "#lib/blurhash.js";

// A person, by their photograph, or their initial where there is none.
let {
	id,
	name,
	photo,
	blurhashes,
	caption,
}: {
	id: string;
	name: string;
	photo?: string;
	blurhashes?: Record<string, string>;
	caption?: string;
} = $props();
</script>

<a href="/people/{id}" class="group block outline-none">
	<span
		class="card-frame block aspect-[2/3]"
		style={photo ? blurStyle(blurhashes?.[photo]) : undefined}
	>
		{#if photo}
			<Artwork id={photo} shape="poster" sizes="10rem" class="card-picture" />
		{:else}
			<span
				class="font-heading text-ink-3 grid size-full place-items-center pb-10 text-3xl font-bold"
				aria-hidden="true"
			>
				{name.slice(0, 1)}
			</span>
		{/if}
		<span class="card-shade grid gap-0.5">
			<span
				class="text-ink line-clamp-2 text-[0.8125rem] leading-tight font-semibold"
			>
				{name}
			</span>
			{#if caption}
				<span class="text-ink-2 truncate text-xs">{caption}</span>
			{/if}
		</span>
	</span>
</a>
