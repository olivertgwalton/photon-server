<script lang="ts">
import { keepFor, sparkPath } from "#lib/admin/metrics.js";

// A reading over the last few minutes to its latest, without axes: the value
// beside it says how much.
let {
	points,
	label,
}: { points: { at: number; value: number }[]; label: string } = $props();

const width = 120;
const height = 28;
const path = $derived(
	sparkPath(points, points.at(-1)?.at ?? 0, keepFor, width, height),
);
</script>

<svg
	viewBox="0 0 {width} {height}"
	preserveAspectRatio="none"
	class="text-ink-2 h-7 w-full"
	role="img"
	aria-label={label}
>
	<line
		x1="0"
		x2={width}
		y1={height - 0.5}
		y2={height - 0.5}
		class="stroke-line"
		vector-effect="non-scaling-stroke"
	/>
	{#if points.length > 1}
		<path
			d={path}
			fill="none"
			stroke="currentColor"
			stroke-width="1.5"
			stroke-linejoin="round"
			vector-effect="non-scaling-stroke"
		/>
	{/if}
</svg>
