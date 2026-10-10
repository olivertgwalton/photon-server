<script lang="ts">
import DatabaseBackupIcon from "@lucide/svelte/icons/database-backup";
import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
import type { Component } from "svelte";
import { refresh, refreshing } from "#lib/actions.svelte.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";

type Mode = components["schemas"]["RefreshMode"];

const modes: {
	mode: Mode;
	title: string;
	detail: Record<typeof refreshing.of, string>;
	Icon: Component;
}[] = [
	{
		mode: "missing",
		title: "Refresh missing metadata",
		detail: {
			library:
				"Only fetch what's missing: films and shows that were never matched, have no overview or poster, have a new season, or failed last time.",
			title:
				"Update it from its providers, and fill in any season or episode that has no details yet. Episodes that already have details are left alone.",
		},
		Icon: RefreshCwIcon,
	},
	{
		mode: "all",
		title: "Refresh all metadata",
		detail: {
			library:
				"Fetch everything again for every film and show, including all their seasons and episodes. A large library can take hours.",
			title:
				"Fetch everything again from its providers, including every season and episode if it's a show.",
		},
		Icon: DatabaseBackupIcon,
	},
];

let asking = $state(false);

async function choose(mode: Mode) {
	asking = true;
	if (await refresh(mode)) refreshing.open = false;
	asking = false;
}
</script>

<Dialog.Root bind:open={refreshing.open}>
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>Refresh metadata</Dialog.Title>
			<Dialog.Description>
				Choose how much of {refreshing.name} to update from its metadata
				providers. Your edits, locked fields and chosen artwork are kept.
			</Dialog.Description>
		</Dialog.Header>
		<div class="grid gap-3">
			{#each modes as { mode, title, detail, Icon } (mode)}
				<button
					type="button"
					disabled={asking}
					onclick={() => choose(mode)}
					class="border-line hover:bg-raise focus-visible:outline-signal flex w-full items-start gap-3 rounded-xl border p-4 text-left transition-colors disabled:opacity-60"
				>
					<Icon class="text-ink-3 mt-0.5 size-5 shrink-0" aria-hidden="true" />
					<span class="grid gap-1">
						<span class="text-ink font-semibold">{title}</span>
						<span class="text-ink-2 text-sm">{detail[refreshing.of]}</span>
					</span>
				</button>
			{/each}
		</div>
		<Dialog.Footer>
			<Dialog.Close>
				{#snippet child({
					props,
				})}
					<Button variant="outline" {...props}>Cancel</Button>
				{/snippet}
			</Dialog.Close>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
