<script lang="ts">
import { artworkURL } from "#lib/artwork.js";
import * as Avatar from "#lib/components/ui/avatar/index.js";
import { cn } from "#lib/utils.js";

// A profile's face: its picture, else its initial. It is drawn beside the
// name, so it says nothing to a screen reader.
let {
	name,
	avatar,
	class: className,
}: { name: string; avatar?: string | null; class?: string } = $props();
</script>

<Avatar.Root
	class={cn("rounded-xl after:rounded-xl", className)}
	aria-hidden="true"
>
	<!-- Always drawn: with no picture it tells the root so, and the initial
	shows again where one was taken away. One size for every place a face is
	drawn, so it is made once. -->
	<Avatar.Image
		src={avatar ? artworkURL(avatar, 320) : undefined}
		alt=""
		class="rounded-[inherit]"
	/>
	<Avatar.Fallback
		class="bg-raise text-ink font-heading rounded-xl text-[length:inherit] font-bold transition-colors"
	>
		{name.slice(0, 1).toUpperCase()}
	</Avatar.Fallback>
</Avatar.Root>
