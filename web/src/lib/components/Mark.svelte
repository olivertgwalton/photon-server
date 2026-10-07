<script lang="ts">
// Photon's mark, as the website draws it: a ribbon folded into a triangle that
// reads as a play glyph, a rounded triangle with one taken out of it, filled
// even-odd, so it is crisp at any size. Its three stops are sampled from the
// app's icon, the one hue on the page and barely one.
let { class: className = "" }: { class?: string } = $props();

const id = $props.id();
</script>

<svg
	width="24"
	height="24"
	viewBox="0 0 100 100"
	class={className}
	aria-hidden="true"
>
	<defs>
		<linearGradient
			id="mark-{id}"
			x1="82"
			y1="14"
			x2="30"
			y2="90"
			gradientUnits="userSpaceOnUse"
		>
			<stop offset="0" stop-color="oklch(0.95 0.012 274)" />
			<stop offset="0.55" stop-color="oklch(0.78 0.028 273)" />
			<stop offset="1" stop-color="oklch(0.58 0.03 271)" />
		</linearGradient>
	</defs>
	<path
		fill="url(#mark-{id})"
		fill-rule="evenodd"
		d="M81.28 58.50 L41.72 81.34 Q27.00 89.84 27.00 72.84 L27.00 27.16 Q27.00 10.16 41.72 18.66 L81.28 41.50 Q96.00 50.00 81.28 58.50 Z
		   M39.00 38.95 L39.00 61.05 Q39.00 69.05 45.93 65.05 L65.07 54.00 Q72.00 50.00 65.07 46.00 L45.93 34.95 Q39.00 30.95 39.00 38.95 Z"
	/>
</svg>
