<script lang="ts">
import { toast } from "svelte-sonner";
import { goto } from "$app/navigation";
import { page } from "$app/state";
import PageHeader from "#lib/components/PageHeader.svelte";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import { when } from "#lib/admin/words.js";
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import { leaveFor, refusalMessage } from "#lib/signin.js";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";

let { data } = $props();
const api = client();
const here = "/settings/sign-in";

// A provider sends the reader back here, with `linked` naming the provider
// they left for, and `refused` saying why when it didn't link.
$effect(() => {
	const params = page.url.searchParams;
	const linked = data.providers.find((p) => p.slug === params.get("linked"));
	const refused = refusalMessage(params.get("refused"));
	if (!linked && !refused) return;
	if (refused) toast.error(refused);
	else if (linked) toast.success(`Your ${linked.name} account is linked.`);
	goto(here, { replace: true, reset: false });
});

async function link(slug: string) {
	const said = await leaveFor(
		api.POST("/api/v1/profile/sign-in-providers/{slug}/link", {
			params: { path: { slug } },
			body: { to: `${here}?linked=${encodeURIComponent(slug)}` },
		}),
	);
	if (said) toast.error(said);
}

const unlink = (slug: string, name: string) =>
	act(
		api.DELETE("/api/v1/profile/sign-in-providers/{slug}", {
			params: { path: { slug } },
		}),
		`Your ${name} account is unlinked.`,
	);
</script>

<PageHeader
	title="Sign-in"
	description="Link your account at a sign-in provider to log in with it instead of your password."
/>

{#if data.providers.length}
	<div class="grid items-start gap-4 lg:grid-cols-2">
		{#each data.providers as p (p.slug)}
			<Card.Root>
				<Card.Header>
					<Card.Title><h2 class="heading">{p.name}</h2></Card.Title>
					<Card.Description>
						{p.account
							? `You can log in here with your ${p.name} account.`
							: `Link your ${p.name} account to log in with it.`}
					</Card.Description>
					{#if p.account}
						<Card.Action><Badge variant="secondary">Linked</Badge></Card.Action>
					{/if}
				</Card.Header>
				<Card.Content>
					{#if p.account}
						<div class="flex flex-wrap items-center gap-x-4 gap-y-2">
							<p class="min-w-0 flex-1 text-sm">
								Linked as
								<span class="text-ink font-semibold">{p.account.username}</span>
								since {when.format(new Date(p.account.linked_at))}
							</p>
							<ConfirmButton
								label="Unlink"
								hidden={p.name}
								title="Unlink {p.name}?"
								body="You can no longer log in here with your {p.name} account, and devices you signed in with it, or paired from one, are signed out. If you have no password yet, set one first."
								confirm="Unlink"
								onconfirm={() => unlink(p.slug, p.name)}
							/>
						</div>
					{:else}
						<div>
							<Button onclick={() => link(p.slug)}>Link {p.name}</Button>
						</div>
					{/if}
				</Card.Content>
			</Card.Root>
		{/each}
	</div>
{:else}
	<p class="text-ink-3 text-sm">
		{#if data.me.role === "admin"}
			No sign-in provider is set up yet: add one under
			<a class="text-ink underline" href="/settings/server/sign-in">Sign-in</a>
			in the server's settings.
		{:else}
			An admin sets up a sign-in provider before you can link an account.
		{/if}
	</p>
{/if}
