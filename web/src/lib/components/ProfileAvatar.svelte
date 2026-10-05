<script lang="ts">
import * as Avatar from "#lib/components/ui/avatar/index.js";
import { cn } from "#lib/utils.js";

// A profile's face: its initial, as profiles have no pictures. It is drawn
// beside the name, so it says nothing to a screen reader.
let { name, class: className }: { name: string; class?: string } = $props();
</script>

<Avatar.Root
	class={cn("rounded-xl after:rounded-xl", className)}
	aria-hidden="true"
>
	<Avatar.Fallback
		class="bg-raise text-ink font-heading rounded-xl text-[length:inherit] font-bold transition-colors"
	>
		{name.slice(0, 1).toUpperCase()}
	</Avatar.Fallback>
</Avatar.Root>
