<script lang="ts">
import type { HTMLImgAttributes } from "svelte/elements";
import { type ArtworkShape, artworkSrc, artworkSrcset } from "#lib/artwork.js";
import { fadeIn } from "#lib/fade.js";

// A picture the server keeps, by its id, at the widths its shape is asked for
// at. It is marked `data-loading` until it arrives, for its class to fade it
// in on.
let {
	id,
	shape,
	alt = "",
	loading = "lazy",
	...rest
}: Omit<HTMLImgAttributes, "src" | "srcset"> & {
	id: string;
	shape: ArtworkShape;
} = $props();
</script>

<img
	{@attach fadeIn}
	src={artworkSrc(id, shape)}
	srcset={artworkSrcset(id, shape)}
	{alt}
	{loading}
	decoding="async"
	{...rest}
>
