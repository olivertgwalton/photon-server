<script lang="ts">
import ArrowDownIcon from "@lucide/svelte/icons/arrow-down";
import ArrowUpIcon from "@lucide/svelte/icons/arrow-up";
import GripVerticalIcon from "@lucide/svelte/icons/grip-vertical";
import type { components } from "#lib/api/schema.js";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import { Checkbox } from "#lib/components/ui/checkbox/index.js";

// A built-in's name or plugin:<slug>.
type Source = components["schemas"]["FieldSource"];

// Where a library takes something from, most trusted first. A source left
// unticked keeps its place but is not asked; one the library has never
// ranked comes last, unticked. What an admin edits by hand outranks them all.
let {
	name,
	labelledby,
	offered,
	chosen,
}: {
	name: string;
	labelledby: string;
	offered: { id: Source; name: string; ready: boolean }[];
	chosen: { source: Source; enabled: boolean }[];
} = $props();

const initial = () => [
	...chosen.flatMap((c) => {
		const o = offered.find((x) => x.id === c.source);
		return o ? [{ ...o, used: c.enabled }] : [];
	}),
	...offered
		.filter((o) => !chosen.some((c) => c.source === o.id))
		.map((o) => ({ ...o, used: false })),
];
let ranked = $state(initial());
let dragging = $state<number>();

function move(from: number, to: number) {
	if (to < 0 || to >= ranked.length || from === to) return;
	const next = [...ranked];
	const [moved] = next.splice(from, 1);
	if (moved) next.splice(to, 0, moved);
	ranked = next;
}
</script>

<input
	type="hidden"
	{name}
	value={JSON.stringify(ranked.map((s) => ({ source: s.id, enabled: s.used })))}
>
<ol
	class="divide-line border-line divide-y rounded-lg border"
	aria-labelledby={labelledby}
>
	{#each ranked as source, i (source.id)}
		<li
			class={[
				"flex items-center gap-3 px-3 py-2",
				dragging === i && "opacity-50",
			]}
			draggable="true"
			ondragstart={(e) => {
				dragging = i;
				e.dataTransfer?.setData("text/plain", String(source.id));
			}}
			ondragover={(e) => e.preventDefault()}
			ondrop={(e) => {
				e.preventDefault();
				if (dragging !== undefined) move(dragging, i);
				dragging = undefined;
			}}
			ondragend={() => (dragging = undefined)}
		>
			<GripVerticalIcon
				class="text-ink-3 size-4 shrink-0 cursor-grab"
				aria-hidden="true"
			/>
			<Checkbox id="{name}-{source.id}" bind:checked={source.used} />
			<label for="{name}-{source.id}" class="text-ink min-w-0 flex-1 text-sm">
				{source.name}
			</label>
			{#if !source.ready}
				<Badge variant="outline">Needs settings</Badge>
			{/if}
			<Button
				variant="ghost"
				size="icon-sm"
				aria-label="Trust {source.name} more"
				disabled={i === 0}
				onclick={() => move(i, i - 1)}
			>
				<ArrowUpIcon />
			</Button>
			<Button
				variant="ghost"
				size="icon-sm"
				aria-label="Trust {source.name} less"
				disabled={i === ranked.length - 1}
				onclick={() => move(i, i + 1)}
			>
				<ArrowDownIcon />
			</Button>
		</li>
	{/each}
</ol>
