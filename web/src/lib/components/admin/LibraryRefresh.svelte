<script lang="ts">
import DatabaseBackupIcon from "@lucide/svelte/icons/database-backup";
import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
import type { Component } from "svelte";
import { act } from "#lib/act.js";
import IconButton from "#lib/components/IconButton.svelte";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { Button, buttonVariants } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";

type Mode = components["schemas"]["RefreshMode"];

// Asks a library's providers about its titles again, as Jellyfin's Refresh
// metadata on a library does: what is missing, or everything. With an icon,
// its button is shown as it.
let { id, name, icon }: { id: string; name: string; icon?: Component } =
	$props();

let open = $state(false);
let asking = $state(false);

const modes: {
	mode: Mode;
	title: string;
	detail: string;
	said: string;
	Icon: Component;
}[] = [
	{
		mode: "missing",
		title: "Refresh missing metadata",
		detail:
			"Films and shows never matched, missing an overview or poster, with a season not yet described, or whose last refresh failed.",
		said: "is being filled in where it's missing.",
		Icon: RefreshCwIcon,
	},
	{
		mode: "all",
		title: "Refresh all metadata",
		detail:
			"Every film and show described again by its providers, with its seasons and episodes. A large library can take hours. Edits, locked fields and chosen artwork stay as they are.",
		said: "is being described again from its providers.",
		Icon: DatabaseBackupIcon,
	},
];

async function refresh(mode: Mode, said: string) {
	asking = true;
	const asked = client().POST("/api/v1/admin/libraries/{id}/refresh", {
		params: { path: { id } },
		body: { mode },
	});
	if (await act(asked, `${name} ${said}`)) open = false;
	asking = false;
}
</script>

<Dialog.Root bind:open>
	{#if icon}
		<Dialog.Trigger>
			{#snippet child({
				props,
			})}
				<IconButton
					{...props}
					label="Refresh metadata"
					hidden="of {name}"
					{icon}
				/>
			{/snippet}
		</Dialog.Trigger>
	{:else}
		<Dialog.Trigger class={buttonVariants({ variant: "outline", size: "sm" })}>
			Refresh metadata <span class="sr-only">of {name}</span>
		</Dialog.Trigger>
	{/if}
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>Refresh library metadata</Dialog.Title>
			<Dialog.Description>
				Choose how much of {name} to ask its metadata providers about again.
			</Dialog.Description>
		</Dialog.Header>
		<div class="grid gap-3">
			{#each modes as { mode, title, detail, said, Icon } (mode)}
				<button
					type="button"
					disabled={asking}
					onclick={() => refresh(mode, said)}
					class="border-line hover:bg-raise focus-visible:outline-signal flex w-full items-start gap-3 rounded-xl border p-4 text-left transition-colors disabled:opacity-60"
				>
					<Icon class="text-ink-3 mt-0.5 size-5 shrink-0" aria-hidden="true" />
					<span class="grid gap-1">
						<span class="text-ink font-semibold">{title}</span>
						<span class="text-ink-2 text-sm">{detail}</span>
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
