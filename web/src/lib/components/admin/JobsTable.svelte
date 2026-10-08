<script lang="ts">
import type { Snippet } from "svelte";
import { vocabulary } from "#lib/vocabulary.js";
import type { components } from "#lib/api/schema.js";
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

const states: Schemas["JobState"][] = ["queued", "running", "rerun", "dead"];

function count(kind: Schemas["JobKind"], state: Schemas["JobState"]) {
	return counts.find((c) => c.kind === kind && c.state === state)?.count ?? 0;
}

const words = vocabulary();
</script>

<Table.Root>
	<Table.Header>
		<Table.Row>
			<Table.Head>Kind</Table.Head>
			{#each states as state (state)}
				<Table.Head class="text-right">{words.job_states[state]}</Table.Head>
			{/each}
			{#if after}
				<Table.Head class="text-right">{afterLabel}</Table.Head>
			{/if}
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each kinds as kind (kind)}
			<Table.Row>
				<Table.Cell class="text-ink font-semibold"
					>{words.jobs[kind]}</Table.Cell
				>
				{#each states as state (state)}
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
