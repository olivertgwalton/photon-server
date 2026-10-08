<script lang="ts">
import CircleAlertIcon from "@lucide/svelte/icons/circle-alert";
import CircleCheckIcon from "@lucide/svelte/icons/circle-check";
import { toast } from "svelte-sonner";
import { refreshAll } from "$app/navigation";
import PageHeader from "#lib/components/PageHeader.svelte";
import { ticking } from "#lib/admin/clock.svelte.js";
import { relative } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/admin/Choice.svelte";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { Progress } from "#lib/components/ui/progress/index.js";
import { act } from "#lib/act.js";
import { fields } from "#lib/form.js";

let { data } = $props();

type Kind = components["schemas"]["StorageKind"];
type Delivery = components["schemas"]["Delivery"];

const kinds: readonly { value: Kind; label: string }[] = [
	{ value: "disk", label: "Each server's own disk" },
	{ value: "bucket", label: "An S3-compatible bucket" },
];

const deliveries: readonly { value: Delivery; label: string }[] = [
	{ value: "proxy", label: "This server" },
	{ value: "redirect", label: "The bucket directly" },
];

// What is chosen, until a save loads the page again.
let kind = $derived<Kind>(data.storage.kind);
let delivery = $derived<Delivery>(data.storage.bucket?.delivery ?? "proxy");
const bucket = $derived(data.storage.bucket);
const move = $derived(data.storage.move);
let checking = $state(false);

// A move's progress is read again every few seconds until it is done.
$effect(() => {
	if (!move) return;
	const timer = setInterval(() => refreshAll(), 2000);
	return () => clearInterval(timer);
});

const clock = ticking(30_000);

// Where a copy is from: a server's own disk, by its name, or the bucket every server shares.
function from(node?: string) {
	if (!node) return "The shared bucket";
	if (node === data.server.node_id) return `This server's disk`;
	const n = data.nodes.find((n) => n.id === node);
	return `${n?.name || "A server"}'s disk`;
}

function place(s: components["schemas"]["StorageStatus"]) {
	return s.kind === "bucket" && s.bucket
		? `the bucket ${s.bucket.name}${s.bucket.folder ? `/${s.bucket.folder}` : ""}`
		: "each server's own disk";
}

const count = new Intl.NumberFormat();

function chosen(form: FormData): components["schemas"]["Storage"] {
	if (kind === "disk") return { kind };
	const text = (name: string) => String(form.get(name) ?? "").trim();
	return {
		kind,
		bucket: {
			endpoint: text("endpoint"),
			name: text("name"),
			folder: text("folder"),
			region: text("region"),
			access_key: text("access_key"),
			secret_key: text("secret_key"),
			delivery,
			public_endpoint: delivery === "redirect" ? text("public_endpoint") : "",
		},
	};
}

async function check(event: MouseEvent) {
	const form = (event.currentTarget as HTMLButtonElement).form;
	if (!form?.reportValidity()) return;
	checking = true;
	const { error } = await client().POST("/api/v1/admin/storage/check", {
		body: chosen(new FormData(form)),
	});
	checking = false;
	if (error) toast.error(problemMessage(error));
	else
		toast.success(
			"The bucket answers, and keeps, lists and removes what is put there.",
		);
}

async function save(event: SubmitEvent) {
	const body = chosen(fields(event));
	const { data: saved, error } = await client().PUT("/api/v1/admin/storage", {
		body,
	});
	if (error) {
		toast.error(problemMessage(error));
		return;
	}
	if (saved.move) {
		toast.success(
			"Moving what is kept. Every server writes to both places until it is copied.",
		);
	} else {
		toast.success("Saved. Every server keeps things there now.");
	}
	// A page may draw from a bucket only once its policy, made as the page loads, names it.
	if (body.bucket?.delivery === "redirect") location.reload();
	else await refreshAll();
}

// Whether this browser reaches the bucket clients are sent to, by a picture there.
let reach = $state<"trying" | "reached" | "unreached">("trying");
$effect(() => {
	const probe = bucket?.probe;
	if (!probe) return;
	reach = "trying";
	const img = new Image();
	img.onload = () => (reach = "reached");
	img.onerror = () => (reach = "unreached");
	img.src = probe;
	return () => {
		img.onload = img.onerror = null;
	};
});
const origin = $derived(bucket?.probe ? new URL(bucket.probe).origin : "");
</script>

<PageHeader
	title="Storage"
	description="Where artwork, avatars, theme tunes and previews are kept. One server keeps them on its own disk; several share them in a bucket."
/>

{#if move}
	<Card.Root class="max-w-2xl">
		<Card.Header>
			<Card.Title
				><h2 class="heading">Moving to {place(move.to)}</h2></Card.Title
			>
			<Card.Description>
				Begun {relative(move.started, clock.now)}. Every server writes to both
				places, and keeps everything in the new one once each copy is done.
				Nothing can be chosen until then.
			</Card.Description>
		</Card.Header>
		<Card.Content class="grid gap-4">
			{#each move.copies as copy (copy.node ?? "shared")}
				{@const label = from(copy.node)}
				<div class="grid gap-1.5 text-sm">
					<div class="flex justify-between gap-4">
						<span class="text-ink">{label}</span>
						<span class="text-ink-3">
							{#if copy.done}
								Copied
							{:else if copy.total}
								{count.format(copy.copied)}
								of {count.format(copy.total)}
							{:else}
								Listing what to copy
							{/if}
						</span>
					</div>
					<Progress
						value={copy.done ? 1 : copy.total ? copy.copied / copy.total : 0}
						max={1}
						aria-label={`Copying ${label}`}
					/>
				</div>
			{:else}
				<p class="text-ink-3 text-sm">Each server is starting its copy.</p>
			{/each}
		</Card.Content>
		<div class="px-6">
			<ConfirmButton
				label="Cancel move"
				title="Cancel the move?"
				confirm="Cancel move"
				onconfirm={() =>
					act(
						client().DELETE("/api/v1/admin/storage/move"),
						"Cancelled. Things stay where they were kept.",
					)}
			>
				Every server keeps things where they are kept now. What was copied stays
				where it was copied to.
			</ConfirmButton>
		</div>
	</Card.Root>
{:else}
	<form onsubmit={save} class="grid max-w-2xl gap-6">
		<Field.Set>
			<Field.Legend>Kept on</Field.Legend>
			<Field.Description>
				A bucket lets every server show the same avatars and previews, and fetch
				each picture once. Choosing another place moves what is kept there.
			</Field.Description>
			<Field.Field>
				<Field.Label for="kind" class="sr-only">Kept on</Field.Label>
				<Choice
					id="kind"
					name="kind"
					bind:value={kind}
					options={kinds}
					class="w-64"
				/>
			</Field.Field>
		</Field.Set>
		{#if kind === "bucket"}
			<Field.Set>
				<Field.Legend>Bucket</Field.Legend>
				<Field.Group>
					<Field.Field>
						<Field.Label for="endpoint">Address</Field.Label>
						<Input
							id="endpoint"
							name="endpoint"
							type="url"
							value={bucket?.endpoint ?? ""}
							placeholder="https://s3.example.com"
							class="font-mono"
						/>
						<Field.Description>Leave empty for Amazon S3.</Field.Description>
					</Field.Field>
					<div class="grid gap-4 sm:grid-cols-2">
						<Field.Field>
							<Field.Label for="name">Name</Field.Label>
							<Input
								id="name"
								name="name"
								required
								value={bucket?.name ?? ""}
								placeholder="photon"
								class="font-mono"
							/>
						</Field.Field>
						<Field.Field>
							<Field.Label for="folder">Folder</Field.Label>
							<Input
								id="folder"
								name="folder"
								value={bucket?.folder ?? ""}
								placeholder="Its root"
								class="font-mono"
							/>
						</Field.Field>
					</div>
					<Field.Field>
						<Field.Label for="region">Region</Field.Label>
						<Input
							id="region"
							name="region"
							value={bucket?.region ?? ""}
							placeholder="Found by itself"
							class="w-64 font-mono"
						/>
					</Field.Field>
				</Field.Group>
			</Field.Set>
			<Field.Set>
				<Field.Legend>Credentials</Field.Legend>
				<Field.Description>
					Leave both empty to sign with the server's own AWS credentials: its
					environment, shared credentials file, or instance role.
				</Field.Description>
				<div class="grid gap-4 sm:grid-cols-2">
					<Field.Field>
						<Field.Label for="access-key">Access key</Field.Label>
						<Input
							id="access-key"
							name="access_key"
							autocomplete="off"
							value={bucket?.access_key ?? ""}
							class="font-mono"
						/>
					</Field.Field>
					<Field.Field>
						<Field.Label for="secret-key">Secret key</Field.Label>
						<Input
							id="secret-key"
							name="secret_key"
							type="password"
							autocomplete="new-password"
							placeholder={bucket?.secret_key_set
								? "Saved; type to replace it"
								: ""}
						/>
					</Field.Field>
				</div>
			</Field.Set>
			<Field.Set>
				<Field.Legend>Clients load pictures from</Field.Legend>
				<Field.Description>
					The bucket directly spares this server sending them, where clients can
					reach the bucket. Anything that could run a script still comes from
					this server.
				</Field.Description>
				<Field.Group>
					<Field.Field>
						<Field.Label for="delivery" class="sr-only"
							>Clients load pictures from</Field.Label
						>
						<Choice
							id="delivery"
							name="delivery"
							bind:value={delivery}
							options={deliveries}
							class="w-64"
						/>
					</Field.Field>
					{#if delivery === "redirect"}
						<Field.Field>
							<Field.Label for="public-endpoint"
								>Address clients reach it at</Field.Label
							>
							<Input
								id="public-endpoint"
								name="public_endpoint"
								type="url"
								value={bucket?.public_endpoint ?? ""}
								placeholder="The address above"
								class="font-mono"
							/>
							<Field.Description>
								Where it differs from the address above, as a store inside
								Docker reached by clients at another name.
							</Field.Description>
						</Field.Field>
					{/if}
				</Field.Group>
			</Field.Set>
			{#if bucket?.probe && data.storage.kind === "bucket"}
				<p role="status" class="flex items-start gap-2 text-sm">
					{#if reach === "reached"}
						<CircleCheckIcon
							class="text-ink-2 mt-0.5 size-4 shrink-0"
							aria-hidden="true"
						/>
						This browser loads pictures from the bucket.
					{:else if reach === "unreached"}
						<CircleAlertIcon
							class="text-destructive mt-0.5 size-4 shrink-0"
							aria-hidden="true"
						/>
						<span>
							This browser can't load pictures from the bucket at {origin}. Set
							the address clients reach it at, or have clients load pictures
							from this server.
						</span>
					{:else}
						Trying the bucket from this browser…
					{/if}
				</p>
			{/if}
		{/if}
		<div class="flex gap-2">
			{#if kind === "bucket"}
				<Button
					type="button"
					variant="outline"
					disabled={checking}
					onclick={check}
				>
					{checking ? "Checking…" : "Check"}
				</Button>
			{/if}
			<Button type="submit">Save</Button>
		</div>
	</form>
{/if}
