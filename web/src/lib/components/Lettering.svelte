<script lang="ts">
import Artwork from "#lib/components/Artwork.svelte";
import { inkOf, letteringHeight } from "#lib/lettering.js";

// A title's lettering, as the apps draw it: its ink alone, cut from the clear
// margins a logo is often set on so it lines up with the words beside it, and
// given the area a 2.5:1 wordmark takes at the box's full height, so a long
// one and a stacked one read at one weight. The box is the --lettering-h and
// --lettering-w its class sets. Until its ink is read it is held unseen, so it
// never jumps; a picture that cannot be read fits the box as it comes, at
// --lettering-align.
let {
	id,
	alt,
	class: className,
}: { id: string; alt: string; class?: string } = $props();
</script>

{#await inkOf(id)}
	<div class="h-(--lettering-h) {className}" aria-hidden="true"></div>
{:then ink}
	{#if ink}
		<div
			role="img"
			aria-label={alt}
			class={["relative", ink.dark && "lettering-glow", className]}
			style:aspect-ratio={ink.aspect}
			style:height="min(calc(var(--lettering-h) * {letteringHeight(
				ink.aspect,
			)}), calc(var(--lettering-w) / {ink.aspect}))"
		>
			<!-- The clear margins spill past the box, where they are nothing. -->
			<Artwork
				{id}
				shape="still"
				loading="eager"
				sizes="26rem"
				class="pointer-events-none absolute max-w-none transition-opacity duration-500 data-loading:opacity-0"
				style="width: {100 / ink.width}%; height: {100 /
					ink.height}%; left: {(-ink.x / ink.width) * 100}%; top: {(-ink.y /
					ink.height) *
					100}%"
			/>
		</div>
	{:else}
		<Artwork
			{id}
			shape="still"
			loading="eager"
			sizes="26rem"
			{alt}
			class="h-(--lettering-h) w-(--lettering-w) object-contain object-(--lettering-align,left) transition-opacity duration-500 data-loading:opacity-0 {className}"
		/>
	{/if}
{/await}
