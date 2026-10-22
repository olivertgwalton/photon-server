<script lang="ts">
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { megabitUnit } from "#lib/format.js";
import { vocabulary } from "#lib/vocabulary.js";

// What a node does, as an admin sets it: taken up by the node at once.
let {
	node,
	open = $bindable(false),
}: { node: components["schemas"]["KnownNode"]; open?: boolean } = $props();

const id = $props.id();
const words = vocabulary();

type Role = components["schemas"]["NodeRole"];
type Acceleration = components["schemas"]["Acceleration"];
type Encoding = "automatic" | Acceleration;
type Limit = "automatic" | "at_most" | "none";
type StreamLimit = "at_most" | "none";

const streamLimits: readonly { value: StreamLimit; label: string }[] = [
	{ value: "none", label: "No limit" },
	{ value: "at_most", label: "At most" },
];

const encodings: readonly { value: Encoding; label: string }[] = [
	{ value: "automatic", label: "Automatic" },
	...Object.entries(words.accelerations).map(([value, label]) => ({
		value: value as Acceleration,
		label,
	})),
];

// The device each acceleration is on, as a node names it, where it may have several.
const devices: Partial<Record<Acceleration, string>> = {
	nvenc: "0",
	qsv: "/dev/dri/renderD128",
	vaapi: "/dev/dri/renderD128",
};

const roles = Object.entries(words.node_roles).map(([value, { name }]) => ({
	value: value as Role,
	label: name,
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
let address = $derived(node.address);
let group = $derived(node.group);
let publicURL = $derived(node.public_url);
let localURL = $derived(node.local_url);
let limit = $derived<Limit>(
	node.transcode_limit_source === "automatic"
		? "automatic"
		: node.transcode_limit
			? "at_most"
			: "none",
);
let most = $derived(node.transcode_limit || node.online?.transcode_limit || 8);
let encoding = $derived<Encoding>(
	node.encoding.source === "set" && node.encoding.acceleration
		? node.encoding.acceleration
		: "automatic",
);
let device = $derived(node.encoding.device ?? "");
let streams = $derived<StreamLimit>(node.stream_limit ? "at_most" : "none");
let streamMost = $derived(node.stream_limit || 20);
let egress = $derived<StreamLimit>(node.egress_limit_mbps ? "at_most" : "none");
let egressMost = $derived(node.egress_limit_mbps || 1000);
const name = $derived(node.name || "this node");

async function save(event: SubmitEvent) {
	event.preventDefault();
	const body: components["schemas"]["NodeChange"] = {
		role,
		address,
		public_url: publicURL,
		local_url: localURL,
		group,
		encoding:
			encoding === "automatic"
				? { source: "automatic" }
				: {
						source: "set",
						acceleration: encoding,
						device: encoding in devices ? device : "",
					},
		stream_limit: streams === "none" ? 0 : streamMost,
		egress_limit_mbps: egress === "none" ? 0 : egressMost,
		...(limit === "automatic"
			? { transcode_limit_source: "automatic" }
			: {
					transcode_limit_source: "set",
					transcode_limit: limit === "none" ? 0 : most,
				}),
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
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto">
		<Dialog.Header>
			<Dialog.Title>{name}</Dialog.Title>
			<Dialog.Description
				>What this node does for the server.</Dialog.Description
			>
		</Dialog.Header>
		<form onsubmit={save} class="grid gap-6">
			<Field.Field>
				<Field.Label for="{id}-node-role">Role</Field.Label>
				<Choice
					id="{id}-node-role"
					name="role"
					bind:value={role}
					options={roles}
				/>
				<Field.Description
					>{words.node_roles[role].description}</Field.Description
				>
			</Field.Field>
			<Field.Field>
				<Field.Label for="{id}-node-address">Address</Field.Label>
				<Input
					id="{id}-node-address"
					name="address"
					type="url"
					placeholder="http://10.0.0.5:8640"
					bind:value={address}
					class="font-mono"
				/>
				<Field.Description>
					Where the other nodes reach it, on a private network. Without one it
					serves only the clients that reach it directly.
				</Field.Description>
			</Field.Field>
			<Field.Field>
				<Field.Label for="{id}-node-public-url">Public URL</Field.Label>
				<Input
					id="{id}-node-public-url"
					name="public_url"
					type="url"
					placeholder="https://gpu.example.com"
					bind:value={publicURL}
					class="font-mono"
				/>
				<Field.Description>
					Where clients reach it themselves. A stream it transcodes for another
					node's client is fetched from here, not passed on through that node.
					Without one, its streams go through the node the client asked.
				</Field.Description>
			</Field.Field>
			<Field.Field>
				<Field.Label for="{id}-node-local-url">Local URL</Field.Label>
				<Input
					id="{id}-node-local-url"
					name="local_url"
					type="url"
					placeholder="http://192.168.1.5:8640"
					bind:value={localURL}
					class="font-mono"
				/>
				<Field.Description>
					Where clients on the server's local networks reach it, such as its
					tailnet address where the tailnet is among them. Without one, they use
					its public URL.
				</Field.Description>
			</Field.Field>
			<Field.Field>
				<Field.Label for="{id}-node-group">Group</Field.Label>
				<Input
					id="{id}-node-group"
					name="group"
					placeholder="None"
					bind:value={group}
				/>
				<Field.Description>
					Such as the site it runs at. It asks the nodes of its own group to
					transcode first, so a stream need not cross between sites.
				</Field.Description>
			</Field.Field>
			<Field.Field>
				<Field.Label for="{id}-node-streams">Streams at once</Field.Label>
				<Choice
					id="{id}-node-streams"
					name="streams"
					bind:value={streams}
					options={streamLimits}
				/>
				{#if streams === "at_most"}
					<Input
						id="{id}-node-stream-most"
						name="stream_limit"
						type="number"
						min={1}
						required
						bind:value={streamMost}
						aria-label="Most streams at once"
						class="w-32 font-mono"
					/>
				{/if}
				<Field.Description>
					What it delivers, transcoded or not. Full, it is passed over for the
					next node; no one watching is stopped.
				</Field.Description>
			</Field.Field>
			<Field.Field>
				<Field.Label for="{id}-node-egress">Egress</Field.Label>
				<Choice
					id="{id}-node-egress"
					name="egress"
					bind:value={egress}
					options={streamLimits}
				/>
				{#if egress === "at_most"}
					<div class="flex items-center gap-2">
						<Input
							id="{id}-node-egress-most"
							name="egress_limit_mbps"
							type="number"
							min={1}
							required
							bind:value={egressMost}
							aria-label="Most megabits a second"
							class="w-32 font-mono"
						/>
						<span class="text-ink-2 text-sm">{megabitUnit}</span>
					</div>
				{/if}
				<Field.Description>
					How fast it sends media, measured every few seconds. Past it, it is
					passed over for the next node.
				</Field.Description>
			</Field.Field>
			{#if role !== "serve"}
				<Field.Field>
					<Field.Label for="{id}-node-limit">Transcodes at once</Field.Label>
					<Choice
						id="{id}-node-limit"
						name="limit"
						bind:value={limit}
						options={limits}
					/>
					{#if limit === "at_most"}
						<Input
							id="{id}-node-most"
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
			<Field.Field>
				<Field.Label for="{id}-node-encoder">Encoder</Field.Label>
				<Choice
					id="{id}-node-encoder"
					name="encoder"
					bind:value={encoding}
					options={encodings}
				/>
				{#if encoding !== "automatic" && encoding in devices}
					<Input
						id="{id}-node-device"
						name="device"
						placeholder={devices[encoding]}
						bind:value={device}
						aria-label="Device"
						class="font-mono"
					/>
				{/if}
				<Field.Description>
					What it encodes video on, taken up for the next video it encodes. One
					that encodes no test picture is passed over for the first that does.
				</Field.Description>
			</Field.Field>
			<Dialog.Footer>
				<Button type="submit">Save</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
