<script lang="ts">
import ExternalLinkIcon from "@lucide/svelte/icons/external-link";
import PlayIcon from "@lucide/svelte/icons/play";
import { artworkSrc, artworkSrcset } from "#lib/artwork.js";
import { type Extra, extraKinds } from "#lib/extras.js";
import { playHref, runtime } from "#lib/format.js";
import { fadeIn } from "#lib/fade.js";

// An extra by a still of its video, played here; or a video a provider links
// to elsewhere, by its site's still served from this server, opened there.
let { item, sizes }: { item: Extra; sizes: string } = $props();

const remote = $derived("video" in item);
const image = $derived("extra" in item ? item.extra.image : undefined);
const thumb = $derived("video" in item ? item.video.thumb : undefined);
</script>

<a
	href={"video" in item ? item.video.url : playHref(item.extra.id)}
	target={remote ? "_blank" : undefined}
	rel={remote ? "noopener noreferrer" : undefined}
	class="group block outline-none"
>
	<span class="card-frame grid aspect-video place-items-center">
		{#if thumb}
			<img
				{@attach fadeIn}
				src={artworkSrc(thumb, "still")}
				srcset={artworkSrcset(thumb, "still")}
				{sizes}
				alt=""
				loading="lazy"
				decoding="async"
				class="card-picture"
			>
		{:else if image}
			<img
				{@attach fadeIn}
				src={image}
				alt=""
				loading="lazy"
				decoding="async"
				class="card-picture"
			>
		{:else if remote}
			<ExternalLinkIcon class="text-ink-3 size-6" aria-hidden="true" />
		{:else}
			<PlayIcon class="text-ink-3 size-6" aria-hidden="true" />
		{/if}
	</span>
	{#if "video" in item}
		<span class="text-ink mt-2 block truncate text-sm font-semibold">
			{item.video.name}
		</span>
		<span class="text-ink-3 block truncate text-xs">
			{extraKinds[item.video.extra_kind]}
			· {item.video.site}
			<span class="sr-only">(opens in a new tab)</span>
		</span>
	{:else}
		<span class="text-ink mt-2 block truncate text-sm font-semibold">
			{item.extra.title}
		</span>
		<span class="text-ink-3 block truncate text-xs">
			{[
				extraKinds[item.extra.extra_kind],
				item.extra.duration_ms && runtime(item.extra.duration_ms),
			]
				.filter(Boolean)
				.join(" · ")}
		</span>
	{/if}
</a>
