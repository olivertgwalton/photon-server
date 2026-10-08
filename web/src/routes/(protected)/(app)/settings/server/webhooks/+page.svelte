<script lang="ts">
import { vocabulary } from "#lib/vocabulary.js";
import PageHeader from "#lib/components/PageHeader.svelte";
import { toast } from "svelte-sonner";
import { refreshAll } from "$app/navigation";
import { act } from "#lib/act.js";
import { fields } from "#lib/form.js";
import { when } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import RevealOnce from "#lib/components/admin/RevealOnce.svelte";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import { Checkbox } from "#lib/components/ui/checkbox/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { Label } from "#lib/components/ui/label/index.js";

let { data } = $props();
const words = vocabulary();

const api = client();
let adding = $state(false);
// The secret is answered once, when the webhook is added, and never again.
let added = $state<components["schemas"]["AddedWebhook"]>();

async function add(event: SubmitEvent) {
	const form = fields(event);
	const { data: hook, error } = await api.POST("/api/v1/admin/webhooks", {
		body: {
			url: String(form.get("url") ?? ""),
			events: form
				.getAll("events")
				.map(String) as components["schemas"]["EventKind"][],
		},
	});
	if (error) return toast.error(problemMessage(error));
	adding = false;
	added = hook;
	await refreshAll();
}
</script>

<PageHeader
	title="Webhooks"
	description="Tell another service when something happens here: a play, a sign-in, a new title. A delivery that keeps failing shows under Jobs."
>
	{#snippet actions()}
		<Dialog.Root bind:open={adding}>
			<Dialog.Trigger>
				{#snippet child({
					props,
				})}
					<Button {...props}>Add a webhook</Button>
				{/snippet}
			</Dialog.Trigger>
			<Dialog.Content class="sm:max-w-lg">
				<form onsubmit={add} class="grid gap-6">
					<Dialog.Header>
						<Dialog.Title>Add a webhook</Dialog.Title>
						<Dialog.Description>
							Each event is posted to the address as JSON, signed in the
							X-Photon-Signature header with a secret shown once, when it is
							added.
						</Dialog.Description>
					</Dialog.Header>
					<Field.Group>
						<Field.Field>
							<Field.Label for="webhook-url">Address</Field.Label>
							<Input
								id="webhook-url"
								name="url"
								type="url"
								required
								placeholder="https://example.com/photon"
							/>
						</Field.Field>
						<Field.Set>
							<Field.Legend>Send it when</Field.Legend>
							<div class="grid gap-2 sm:grid-cols-2">
								{#each Object.entries(words.hookable) as [kind, label] (kind)}
									<div class="flex items-center gap-2">
										<Checkbox id="event-{kind}" name="events" value={kind} />
										<Label for="event-{kind}">{label}</Label>
									</div>
								{/each}
							</div>
						</Field.Set>
					</Field.Group>
					<Dialog.Footer>
						<Button type="submit">Add webhook</Button>
					</Dialog.Footer>
				</form>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</PageHeader>

{#if data.webhooks.length}
	<ul class="grid gap-3">
		{#each data.webhooks as hook (hook.id)}
			{@const path = { params: { path: { id: hook.id } } }}
			<li
				class="bg-raise grid gap-3 rounded-xl p-4 sm:grid-cols-[1fr_auto] sm:items-start"
			>
				<div class="grid min-w-0 gap-2">
					<p class="text-ink truncate font-mono text-sm">{hook.url}</p>
					<div class="flex flex-wrap gap-1.5">
						{#each hook.events as kind (kind)}
							<Badge variant="outline">{words.hookable[kind] ?? kind}</Badge>
						{/each}
					</div>
					<p class="text-ink-3 text-xs">
						Added {when.format(new Date(hook.created_at))}
					</p>
				</div>
				<div class="flex gap-2">
					<Button
						variant="outline"
						size="sm"
						onclick={() =>
							act(
								api.POST("/api/v1/admin/webhooks/{id}/test", path),
								"A test was sent. If it can't be delivered it shows under Jobs.",
							)}
					>
						Send a test <span class="sr-only">to {hook.url}</span>
					</Button>
					<ConfirmButton
						label="Remove"
						hidden={hook.url}
						title="Remove this webhook?"
						confirm="Remove webhook"
						onconfirm={() =>
							act(
								api.DELETE("/api/v1/admin/webhooks/{id}", path),
								"The webhook was removed.",
							)}
					>
						{hook.url}
						is told of nothing more, and what was waiting to be sent to it is
						dropped.
					</ConfirmButton>
				</div>
			</li>
		{/each}
	</ul>
{:else}
	<p class="text-ink-3 text-sm">No webhooks yet.</p>
{/if}

<RevealOnce
	value={added?.secret}
	title="The webhook's secret"
	label="Secret"
	copied="The secret was copied."
	onclose={() => (added = undefined)}
>
	Keep it where {added?.url} can check signatures with it. It is not shown
	again.
</RevealOnce>
