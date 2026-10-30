<script lang="ts">
import { act } from "#lib/act.ts";
import { when } from "#lib/admin/words.ts";
import { client } from "#lib/api/client.ts";
import type { components } from "#lib/api/schema.d.ts";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import RevealOnce from "#lib/components/admin/RevealOnce.svelte";
import Choice from "#lib/components/Choice.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";
import { Badge } from "#lib/components/ui/badge/index.ts";
import { Button } from "#lib/components/ui/button/index.ts";
import { Checkbox } from "#lib/components/ui/checkbox/index.ts";
import * as Dialog from "#lib/components/ui/dialog/index.ts";
import * as Field from "#lib/components/ui/field/index.ts";
import { Input } from "#lib/components/ui/input/index.ts";
import { Label } from "#lib/components/ui/label/index.ts";
import { fields, textAsTyped } from "#lib/form.ts";
import { vocabulary } from "#lib/vocabulary.ts";

let { data } = $props();
const words = vocabulary();

const api = client();
let adding = $state(false);
// The secret is answered once, when the webhook is added, and never again;
// only photon's own JSON is signed with it.
let added = $state.raw<components["schemas"]["AddedWebhook"]>();
let format = $state<components["schemas"]["WebhookFormat"]>("photon");
const formats = Object.entries(words.webhook_formats).map(([value, label]) => ({
	value: value as components["schemas"]["WebhookFormat"],
	label,
}));

async function add(event: SubmitEvent) {
	const form = fields(event);
	const hook = await act(
		api.POST("/api/v1/admin/webhooks", {
			body: {
				url: textAsTyped(form, "url"),
				format,
				events: form
					.getAll("events")
					.map(String) as components["schemas"]["EventKind"][],
			},
		}),
	);
	if (!hook) return;
	adding = false;
	if (hook.data.format === "photon") added = hook.data;
}
</script>

<PageHeader
	title="Webhooks"
	description="Tell another service when something happens here: a play, a sign-in, a new title. A delivery that keeps failing shows under Jobs."
>
	{#snippet actions()}
		<Dialog.Root bind:open={adding}>
			<Dialog.Trigger>
				{#snippet child({ props })}
					<Button {...props}>Add a webhook</Button>
				{/snippet}
			</Dialog.Trigger>
			<Dialog.Content class="sm:max-w-lg">
				<form onsubmit={add} class="grid gap-6">
					<Dialog.Header>
						<Dialog.Title>Add a webhook</Dialog.Title>
						<Dialog.Description>
							{#if format === "photon"}
								Each event is posted to the address as JSON, signed in the
								X-Photon-Signature header with a secret shown once, when it is
								added.
							{:else}
								Each event is posted as {words.webhook_formats[format]} takes a
								message: the server's name over what happened. Use the address
								it gives you, with its token.
							{/if}
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
						<Field.Field>
							<Field.Label for="webhook-format">Sent as</Field.Label>
							<Choice
								id="webhook-format"
								options={formats}
								bind:value={format}
								class="w-full sm:w-64"
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
					{#if hook.format !== "photon"}
						<p class="text-ink-3 text-xs">
							Sent as {words.webhook_formats[hook.format]}
						</p>
					{/if}
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
						body={`${hook.url} is told of nothing more, and what was waiting to be sent to it is dropped.`}
					/>
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
