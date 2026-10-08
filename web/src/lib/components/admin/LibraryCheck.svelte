<script lang="ts">
import CircleAlertIcon from "@lucide/svelte/icons/circle-alert";
import CircleCheckIcon from "@lucide/svelte/icons/circle-check";
import FolderCheckIcon from "@lucide/svelte/icons/folder-check";
import LoaderCircleIcon from "@lucide/svelte/icons/loader-circle";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import IconButton from "#lib/components/IconButton.svelte";
import * as Popover from "#lib/components/ui/popover/index.js";

type Check = components["schemas"]["LibraryCheck"];
type Access = components["schemas"]["RootAccess"];

// Checks that every node running can reach and read a library's root, and
// says what each found.
let { id, name }: { id: string; name: string } = $props();

let open = $state(false);
let checking = $state(false);
let check = $state<Check>();
let refused = $state("");

const said: Record<Exclude<Access, "readable">, string> = {
	missing: "Not there",
	not_a_folder: "Not a folder",
	denied: "Not allowed to read it",
	unreadable: "Can't be read",
	timed_out: "Didn't answer in time; its mount may be hung",
	unreachable_node: "This node didn't answer",
};

function entries(n: number) {
	if (n === 0) return "Readable, but empty: is the media mounted?";
	return `Readable, ${n} ${n === 1 ? "entry" : "entries"}`;
}

async function run() {
	checking = true;
	refused = "";
	const { data, error } = await client().POST(
		"/api/v1/admin/libraries/{id}/check",
		{ params: { path: { id } } },
	);
	check = data;
	if (error) refused = problemMessage(error);
	checking = false;
}
</script>

<Popover.Root
	bind:open
	onOpenChange={(o) => {
		if (o) run();
	}}
>
	<Popover.Trigger>
		{#snippet child({
			props,
		})}
			<IconButton
				{...props}
				label="Check access"
				hidden="to {name}"
				icon={checking ? LoaderCircleIcon : FolderCheckIcon}
				aria-busy={checking}
			/>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content
		align="end"
		class="grid w-[min(24rem,calc(100vw-2rem))] grid-cols-1 gap-3"
		aria-label="Access to {name}"
	>
		<Popover.Title class="text-ink font-semibold">
			Access on every node
		</Popover.Title>
		{#if checking}
			<p class="text-ink-2 text-sm" role="status">Checking every node…</p>
		{:else if refused}
			<p class="text-destructive text-sm" role="alert">{refused}</p>
		{:else if check}
			<p
				class="text-ink-3 min-w-0 truncate font-mono text-xs"
				title={check.root}
			>
				{check.root}
			</p>
			<ul class="grid min-w-0 gap-2" role="status">
				{#each check.nodes as node (node.id)}
					{@const ok = node.access === "readable" && node.entries > 0}
					<li class="flex items-start gap-2 text-sm">
						{#if ok}
							<CircleCheckIcon
								class="text-signal mt-0.5 size-4 shrink-0"
								aria-hidden="true"
							/>
						{:else}
							<CircleAlertIcon
								class="text-destructive mt-0.5 size-4 shrink-0"
								aria-hidden="true"
							/>
						{/if}
						<div class="grid min-w-0 gap-0.5">
							<span class="text-ink font-medium">{node.name}</span>
							<span class="text-ink-2">
								{node.access === "readable"
									? entries(node.entries)
									: said[node.access]}
							</span>
							{#if node.error}
								<span class="text-ink-3 font-mono text-xs wrap-anywhere"
									>{node.error}</span
								>
							{/if}
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	</Popover.Content>
</Popover.Root>
