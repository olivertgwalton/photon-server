<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { toast } from "svelte-sonner";
import { refreshAll } from "$app/navigation";
import { act } from "#lib/act.js";
import { fields } from "#lib/form.js";
import { when } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import RevealOnce from "#lib/components/admin/RevealOnce.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

let { data } = $props();

const api = client();
let adding = $state(false);
// The token is answered once, when the key is made: the server keeps only its hash.
let made = $state<{ name: string; token: string }>();

async function add(event: SubmitEvent) {
	const name = String(fields(event).get("name") ?? "");
	const { data: key, error } = await api.POST("/api/v1/admin/keys", {
		body: { name },
	});
	if (error) return toast.error(problemMessage(error));
	adding = false;
	made = { name, token: key.token };
	await refreshAll();
}
</script>

<PageHeader
	title="API keys"
	description="Let a script or another server reach this one. A key acts as the admin who made it, with that profile's role, until it is revoked."
>
	{#snippet actions()}
		<Dialog.Root bind:open={adding}>
			<Dialog.Trigger>
				{#snippet child({
					props,
				})}
					<Button {...props}>Make a key</Button>
				{/snippet}
			</Dialog.Trigger>
			<Dialog.Content class="sm:max-w-lg">
				<form onsubmit={add} class="grid gap-6">
					<Dialog.Header>
						<Dialog.Title>Make an API key</Dialog.Title>
						<Dialog.Description>
							It is sent as a bearer token in the Authorization header, and
							shown once, when it is made.
						</Dialog.Description>
					</Dialog.Header>
					<Field.Group>
						<Field.Field>
							<Field.Label for="key-name">What it is for</Field.Label>
							<Input
								id="key-name"
								name="name"
								required
								maxlength={64}
								placeholder="Sonarr"
							/>
						</Field.Field>
					</Field.Group>
					<Dialog.Footer>
						<Button type="submit">Make key</Button>
					</Dialog.Footer>
				</form>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</PageHeader>

{#if data.keys.length}
	<ul class="grid gap-3">
		{#each data.keys as key (key.id)}
			<li
				class="bg-raise grid gap-3 rounded-xl p-4 sm:grid-cols-[1fr_auto] sm:items-start"
			>
				<div class="grid min-w-0 gap-1">
					<p class="text-ink truncate font-semibold">{key.name}</p>
					<p class="text-ink-3 text-xs">
						Made by {key.profile}
						{when.format(new Date(key.created_at))}
						· last used
						{when.format(new Date(key.last_seen_at))}
					</p>
				</div>
				<ConfirmButton
					label="Revoke"
					hidden={key.name}
					title="Revoke this key?"
					confirm="Revoke key"
					onconfirm={() =>
						act(
							api.DELETE("/api/v1/admin/keys/{id}", {
								params: { path: { id: key.id } },
							}),
							"The key was revoked.",
						)}
				>
					Whatever uses the {key.name} key is refused from now on.
				</ConfirmButton>
			</li>
		{/each}
	</ul>
{:else}
	<p class="text-ink-3 text-sm">No API keys yet.</p>
{/if}

<RevealOnce
	value={made?.token}
	title="The new API key"
	label="Key"
	copied="The key was copied."
	onclose={() => (made = undefined)}
>
	Give it to {made?.name}. It is not shown again.
</RevealOnce>
