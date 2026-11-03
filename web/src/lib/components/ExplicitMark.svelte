<script lang="ts">
import type { components } from "#lib/api/schema.d.ts";
import { Badge } from "#lib/components/ui/badge/index.ts";

// Apple Music's mark of an explicit track, a small E beside its title. A clean
// track, or one whose file says neither, has none.
let { advisory }: { advisory?: components["schemas"]["Advisory"] } = $props();
</script>

{#if advisory === "explicit"}
	<Badge
		variant="secondary"
		role="img"
		aria-label="Explicit"
		title="Explicit"
		class="h-4 min-w-4 rounded-sm px-1 text-[0.625rem] leading-none font-semibold"
	>
		<span aria-hidden="true">E</span>
	</Badge>
{/if}
