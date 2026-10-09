<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import SignInProviderForm from "#lib/components/admin/SignInProviderForm.svelte";
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";

let { data } = $props();
let adding = $state(false);
// The slug of the provider being changed.
let editing = $state<string>();

const remove = (slug: string, name: string) =>
	act(
		client().DELETE("/api/v1/admin/sign-in-providers/{slug}", {
			params: { path: { slug } },
		}),
		`${name} was removed.`,
	);
</script>

<PageHeader
	title="Sign-in"
	description="Let the household log in through an OpenID Connect provider such as Authelia, Authentik, Keycloak or Pocket ID. Register this server there as a client using the authorization code flow, then add it here."
>
	{#snippet actions()}
		<Dialog.Root bind:open={adding}>
			<Dialog.Trigger>
				{#snippet child({
					props,
				})}
					<Button {...props} disabled={!data.publicURL}>Add a provider</Button>
				{/snippet}
			</Dialog.Trigger>
			<Dialog.Content class="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
				<Dialog.Header>
					<Dialog.Title>Add a sign-in provider</Dialog.Title>
					<Dialog.Description>
						The server checks the provider answers at its issuer before it's
						added.
					</Dialog.Description>
				</Dialog.Header>
				<SignInProviderForm
					publicURL={data.publicURL}
					libraries={data.libraries}
					onsaved={() => (adding = false)}
				/>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</PageHeader>

{#if !data.publicURL}
	<p class="text-ink-2 text-sm">
		Set the server's public address under
		<a class="text-ink underline" href="/settings/server/network">Network</a>
		first: a provider sends people back to it once they've signed in.
	</p>
{/if}

{#if data.providers.length}
	<div class="grid items-start gap-4 lg:grid-cols-2">
		{#each data.providers as p (p.slug)}
			<Card.Root>
				<Card.Header>
					<Card.Title><h2 class="heading">{p.name}</h2></Card.Title>
					<Card.Description class="font-mono [overflow-wrap:anywhere]">
						{p.issuer}
					</Card.Description>
					<Card.Action>
						<Badge variant="outline">
							{p.provisioning === "create" ? "Makes profiles" : "Linked only"}
						</Badge>
					</Card.Action>
				</Card.Header>
				<Card.Content class="grid gap-4">
					<dl
						class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm"
					>
						<dt class="text-ink-3">Client id</dt>
						<dd class="font-mono [overflow-wrap:anywhere]">{p.client_id}</dd>
						<dt class="text-ink-3">Client secret</dt>
						<dd>{p.client_secret_set ? "Kept" : "None: a public client"}</dd>
						<dt class="text-ink-3">Checks back</dt>
						<dd>{p.recheck === "hourly" ? "Every hour" : "Only at sign-in"}</dd>
						{#if p.group}
							<dt class="text-ink-3">Required group</dt>
							<dd class="[overflow-wrap:anywhere]">{p.group}</dd>
						{/if}
						{#if p.provisioning === "create"}
							<dt class="text-ink-3">Profiles it makes see</dt>
							<dd>
								{p.access.libraries.length
									? data.libraries
											.filter((l) => p.access.libraries.includes(l.id))
											.map((l) => l.name)
											.join(", ")
									: "Every library"}{p.access.max_age == null
									? ""
									: `, up to ${p.access.max_age}`}
							</dd>
						{/if}
						{#if p.callback_url}
							<dt class="text-ink-3">Redirect URI</dt>
							<dd class="font-mono [overflow-wrap:anywhere]">
								{p.callback_url}
							</dd>
						{/if}
					</dl>
					<div class="flex flex-wrap gap-2">
						<Dialog.Root
							open={editing === p.slug}
							onOpenChange={(open) => (editing = open ? p.slug : undefined)}
						>
							<Dialog.Trigger>
								{#snippet child({
									props,
								})}
									<Button {...props} variant="outline" size="sm">
										Change <span class="sr-only">{p.name}</span>
									</Button>
								{/snippet}
							</Dialog.Trigger>
							<Dialog.Content class="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
								<Dialog.Header>
									<Dialog.Title>Change {p.name}</Dialog.Title>
									<Dialog.Description>
										Moving it to another issuer unlinks every account linked at
										the old one, and signs out the devices they signed in.
									</Dialog.Description>
								</Dialog.Header>
								<SignInProviderForm
									provider={p}
									publicURL={data.publicURL}
									libraries={data.libraries}
									onsaved={() => (editing = undefined)}
								/>
							</Dialog.Content>
						</Dialog.Root>
						<ConfirmButton
							label="Remove"
							hidden={p.name}
							title="Remove {p.name}?"
							body="No one can log in with {p.name} any more: every account linked there is unlinked, and the devices they signed in are signed out. A profile with no password of its own can log in again once you set it one under Profiles."
							confirm="Remove provider"
							onconfirm={() => remove(p.slug, p.name)}
						/>
					</div>
				</Card.Content>
			</Card.Root>
		{/each}
	</div>
{:else if data.publicURL}
	<p class="text-ink-3 text-sm">No sign-in providers yet.</p>
{/if}
