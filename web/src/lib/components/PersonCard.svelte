<script lang="ts">
import { artworkSrc, artworkSrcset } from "#lib/artwork.js";

// A person, by their photograph, or their initial where there is none.
let {
	id,
	name,
	photo,
	caption,
}: { id: string; name: string; photo?: string; caption?: string } = $props();
</script>

<a href="/people/{id}" class="group block outline-none">
	<span
		class="bg-raise group-hover:ring-line-strong group-focus-visible:ring-signal block aspect-[2/3] overflow-hidden rounded-lg ring-2 ring-transparent"
	>
		{#if photo}
			<img
				src={artworkSrc(photo, "poster")}
				srcset={artworkSrcset(photo, "poster")}
				sizes="10rem"
				alt=""
				loading="lazy"
				decoding="async"
				class="size-full object-cover"
			>
		{:else}
			<span
				class="font-heading text-ink-3 grid size-full place-items-center text-3xl font-bold"
				aria-hidden="true"
			>
				{name.slice(0, 1)}
			</span>
		{/if}
	</span>
	<span class="text-ink mt-2 block truncate text-sm font-semibold">{name}</span>
	{#if caption}
		<span class="text-ink-3 block truncate text-xs">{caption}</span>
	{/if}
</a>
