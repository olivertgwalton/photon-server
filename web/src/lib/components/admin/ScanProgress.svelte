<script lang="ts">
import type { components } from "#lib/api/schema.js";
import { Progress } from "#lib/components/ui/progress/index.js";

// A library's scan as it goes. The walk counts folders as it finds them, so
// the bar's end moves out while it fills.
let { scan, name }: { scan: components["schemas"]["Scan"]; name: string } =
	$props();

const said = $derived(
	scan.phase === "reading"
		? `Reading folders: ${scan.done} of ${scan.known}`
		: "Forgetting what is gone",
);
</script>

<div class="grid gap-1.5">
	<p class="text-ink-2 text-sm">{said}</p>
	<Progress
		value={scan.phase === "reading" ? scan.done : null}
		max={Math.max(scan.known, 1)}
		aria-label="Scanning {name}"
	/>
</div>
