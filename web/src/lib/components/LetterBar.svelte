<script lang="ts">
import type { components } from "#lib/api/schema.js";

// The jump bar beside a wall in title order: a letter with nothing under it
// is drawn, so the bar does not move, and cannot be chosen.
let {
	letters,
	onjump,
}: {
	letters: components["schemas"]["Letter"][];
	onjump: (letter: string) => void;
} = $props();

const all = ["#", ..."ABCDEFGHIJKLMNOPQRSTUVWXYZ"];
const counts = $derived(new Map(letters.map((l) => [l.letter, l.count])));
</script>

<nav aria-label="Jump to a letter">
	<ul
		class="flex flex-wrap gap-0.5 md:sticky md:top-20 md:flex-col md:flex-nowrap"
	>
		{#each all as letter (letter)}
			{@const count = counts.get(letter) ?? 0}
			<li>
				<button
					type="button"
					disabled={!count}
					onclick={() => onjump(letter)}
					aria-label={letter === "#"
						? "Titles before A"
						: `Titles starting with ${letter}`}
					class="text-ink-2 hover:bg-accent hover:text-ink disabled:text-ink-3/40 grid size-7 place-items-center rounded font-mono text-xs font-bold disabled:pointer-events-none md:size-6"
				>
					{letter}
				</button>
			</li>
		{/each}
	</ul>
</nav>
