<script lang="ts">
import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
import { act } from "#lib/admin/act.js";
import { relative } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import NodeSettings from "#lib/components/admin/NodeSettings.svelte";
import * as AlertDialog from "#lib/components/ui/alert-dialog/index.js";
import { buttonVariants } from "#lib/components/ui/button/index.js";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Textarea } from "#lib/components/ui/textarea/index.js";

type Node = components["schemas"]["KnownNode"];

// What an admin may do of a node: set what it does, drain it before stopping it, resume it, and
// forget it once taken away.
let { node, nodes }: { node: Node; nodes: Node[] } = $props();

let settings = $state(false);
let draining = $state(false);
let forgetting = $state(false);
let note = $state("");

const streams = $derived(node.online?.transcodes ?? 0);
// Whether any other node up takes transcodes, so new streams still have somewhere to go.
const others = $derived(
	nodes.some(
		(n) =>
			n.id !== node.id &&
			n.online &&
			n.role !== "serve" &&
			n.availability === "active",
	),
);

function set(body: components["schemas"]["NodeChange"], said: string) {
	return act(
		client().PATCH("/api/v1/admin/nodes/{id}", {
			params: { path: { id: node.id } },
			body,
		}),
		said,
	);
}
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger
		class={buttonVariants({ variant: "outline", size: "sm" })}
		aria-label="Actions for {node.name}"
	>
		<EllipsisIcon class="size-4" aria-hidden="true" />
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end" class="w-48">
		<DropdownMenu.Item onSelect={() => (settings = true)}
			>Settings…</DropdownMenu.Item
		>
		{#if node.availability === "active"}
			<DropdownMenu.Item onSelect={() => (draining = true)}
				>Drain…</DropdownMenu.Item
			>
		{:else}
			<DropdownMenu.Item
				onSelect={() =>
					set(
						{ availability: "active" },
						`${node.name} takes new streams again.`,
					)}
			>
				Resume
			</DropdownMenu.Item>
		{/if}
		{#if !node.online}
			<DropdownMenu.Item onSelect={() => (forgetting = true)}
				>Forget…</DropdownMenu.Item
			>
		{/if}
	</DropdownMenu.Content>
</DropdownMenu.Root>

<NodeSettings {node} bind:open={settings} />

<AlertDialog.Root bind:open={draining}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Drain {node.name}?</AlertDialog.Title>
			<AlertDialog.Description>
				{streams
					? `It will finish its ${streams === 1 ? "1 current stream" : `${streams} current streams`} and take no new ones.`
					: "It will take no new streams."}
				{others
					? "Other nodes take new streams meanwhile."
					: "No other node takes streams to transcode, so those will be refused until it is resumed."}
			</AlertDialog.Description>
		</AlertDialog.Header>
		<Field.Field>
			<Field.Label for="drain-note">Note for other admins</Field.Label>
			<Textarea
				id="drain-note"
				bind:value={note}
				rows={2}
				placeholder="Optional"
			/>
		</Field.Field>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				onclick={() => {
					draining = false;
					return set(
						{ availability: "draining", note: note.trim() },
						`${node.name} is draining.`,
					);
				}}
			>
				Drain {node.name}
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root bind:open={forgetting}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Forget {node.name}?</AlertDialog.Title>
			<AlertDialog.Description>
				It was last seen {relative(node.last_seen, Date.now())}. What is set of
				it goes with it; should it start again, it is back as a new node.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				onclick={() => {
					forgetting = false;
					return act(
						client().DELETE("/api/v1/admin/nodes/{id}", {
							params: { path: { id: node.id } },
						}),
						`${node.name} is forgotten.`,
					);
				}}
			>
				Forget {node.name}
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
