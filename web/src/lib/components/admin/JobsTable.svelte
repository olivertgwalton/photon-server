<script lang="ts">
import type { Snippet } from "svelte";
import type { components } from "#lib/api/schema.js";
import { jobKinds } from "#lib/admin/words.js";
import * as Table from "#lib/components/ui/table/index.js";

type Schemas = components["schemas"];

// How many jobs of each kind are in each state, a row a kind. `after` adds a
// column of its own, headed `afterLabel`.
let {
	counts,
	kinds,
	after,
	afterLabel = "",
}: {
	counts: Schemas["JobCount"][];
	kinds: Schemas["JobKind"][];
	after?: Snippet<[Schemas["JobKind"]]>;
	afterLabel?: string;
} = $props();

const states: [Schemas["JobState"], string][] = [
	["queued", "Queued"],
	["running", "Running"],
	["rerun", "To run again"],
	["dead", "Gave up"],
];

function count(kind: Schemas["JobKind"], state: Schemas["JobState"]) {
	return counts.find((c) => c.kind === kind && c.state === state)?.count ?? 0;
}
</script>

<Table.Root>
	<Table.Header>
		<Table.Row>
			<Table.Head>Kind</Table.Head>
			{#each states as [, label] (label)}
				<Table.Head class="text-right">{label}</Table.Head>
			{/each}
			{#if after}
				<Table.Head class="text-right">{afterLabel}</Table.Head>
			{/if}
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each kinds as kind (kind)}
			<Table.Row>
				<Table.Cell class="text-ink font-semibold">{jobKinds[kind]}</Table.Cell>
				{#each states as [state] (state)}
					<Table.Cell class="text-right font-mono"
						>{count(kind, state) || ""}</Table.Cell
					>
				{/each}
				{#if after}
					<Table.Cell class="text-ink-2 text-right"
						>{@render after(kind)}</Table.Cell
					>
				{/if}
			</Table.Row>
		{/each}
	</Table.Body>
</Table.Root>
