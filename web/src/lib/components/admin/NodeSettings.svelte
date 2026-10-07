<script lang="ts">
import { act } from "#lib/act.js";
import { nodeRoles } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/admin/Choice.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

// What a node does, as an admin sets it: taken up by the node at once.
let {
	node,
	open = $bindable(false),
}: { node: components["schemas"]["KnownNode"]; open?: boolean } = $props();

type Role = components["schemas"]["NodeRole"];
type Limit = "automatic" | "at_most" | "none";

const roles = (Object.keys(nodeRoles) as Role[]).map((value) => ({
	value,
	label: nodeRoles[value].name,
}));

// The limit its encoder keeps up with, where the node says it while up.
const worked = $derived(
	node.transcode_limit_source === "automatic"
		? node.online?.transcode_limit
		: undefined,
);
const limits = $derived<readonly { value: Limit; label: string }[]>([
	{
		value: "automatic",
		label: worked
			? `Worked out from its encoder (${worked})`
			: "Worked out from its encoder",
	},
	{ value: "at_most", label: "At most" },
	{ value: "none", label: "No limit" },
]);

let role = $derived<Role>(node.role);
let limit = $derived<Limit>(
	node.transcode_limit_source === "automatic"
		? "automatic"
		: node.transcode_limit
			? "at_most"
			: "none",
);
let most = $derived(node.transcode_limit || node.online?.transcode_limit || 8);
const name = $derived(node.name || "this node");

async function save(event: SubmitEvent) {
	event.preventDefault();
	const body: components["schemas"]["NodeChange"] =
		limit === "automatic"
			? { role, transcode_limit_source: "automatic" }
			: {
					role,
					transcode_limit_source: "set",
					transcode_limit: limit === "none" ? 0 : most,
				};
	const saved = await act(
		client().PATCH("/api/v1/admin/nodes/{id}", {
			params: { path: { id: node.id } },
			body,
		}),
		`Saved. ${name} takes it up at once.`,
	);
	if (saved) open = false;
}
</script>

<Dialog.Root bind:open>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>{name}</Dialog.Title>
			<Dialog.Description>
				{node.online?.address
					? `What this node, at ${node.online.address}, does for the server.`
					: "What this node does for the server."}
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={save} class="grid gap-6">
			<Field.Field>
				<Field.Label for="node-role">Role</Field.Label>
				<Choice id="node-role" name="role" bind:value={role} options={roles} />
				<Field.Description>{nodeRoles[role].description}</Field.Description>
			</Field.Field>
			{#if role !== "serve"}
				<Field.Field>
					<Field.Label for="node-limit">Transcodes at once</Field.Label>
					<Choice
						id="node-limit"
						name="limit"
						bind:value={limit}
						options={limits}
					/>
					{#if limit === "at_most"}
						<Input
							id="node-most"
							name="transcode_limit"
							type="number"
							min={1}
							required
							bind:value={most}
							aria-label="Most transcodes at once"
							class="w-32 font-mono"
						/>
					{/if}
					<Field.Description>
						Lowering it stops no one watching; nothing more begins until there
						is room.
					</Field.Description>
				</Field.Field>
			{/if}
			<Dialog.Footer>
				<Button type="submit">Save</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
