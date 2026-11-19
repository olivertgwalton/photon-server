<script lang="ts">
import { client } from "#lib/api/client.ts";
import { Button } from "#lib/components/ui/button/index.ts";
import * as Card from "#lib/components/ui/card/index.ts";
import * as Field from "#lib/components/ui/field/index.ts";
import { Input } from "#lib/components/ui/input/index.ts";
import { fields } from "#lib/form.ts";
import { LOGIN } from "#lib/session.ts";
import { attempt, browserSession } from "#lib/signin.ts";
import { goto } from "$app/navigation";
import { page } from "$app/state";

// The code rides in the link's fragment, which the browser never sends.
const code = $derived(decodeURIComponent(page.url.hash.slice(1)));
let pending = $state(false);
let message = $state<string>();

async function accept(event: SubmitEvent) {
	const form = fields(event);
	const password = String(form.get("password"));
	if (password !== form.get("confirm")) {
		message = "The passwords don't match.";
		return;
	}
	pending = true;
	const accepted = await attempt(
		client().POST("/api/v1/auth/invites/redemptions", {
			body: { code, password, ...browserSession() },
		}),
	);
	if (!accepted.answer) {
		pending = false;
		message = accepted.message;
		return;
	}
	await goto("/", { replaceState: true });
}
</script>

<svelte:head><title>Accept invite · Photon</title></svelte:head>

<Card.Root class="w-full max-w-sm">
	<Card.Header>
		<Card.Title>
			<h1 class="heading text-xl">Choose your password</h1>
		</Card.Title>
		<Card.Description>
			{#if code}
				You've been invited to a profile here. Choose the password you'll log in
				with.
			{:else}
				This link has no invite in it. Ask whoever sent it for a new one.
			{/if}
		</Card.Description>
	</Card.Header>
	<Card.Content>
		{#if code}
			<form onsubmit={accept}>
				<Field.Group>
					<Field.Field>
						<Field.Label for="password">Password</Field.Label>
						<Input
							id="password"
							name="password"
							type="password"
							autocomplete="new-password"
							minlength={8}
							required
						/>
						<Field.Description>At least 8 characters.</Field.Description>
					</Field.Field>
					<Field.Field>
						<Field.Label for="confirm">Confirm password</Field.Label>
						<Input
							id="confirm"
							name="confirm"
							type="password"
							autocomplete="new-password"
							required
						/>
					</Field.Field>
					<Field.Error errors={[{ message }]} />
					<Field.Field>
						<Button type="submit" disabled={pending}>Set password</Button>
					</Field.Field>
				</Field.Group>
			</form>
		{/if}
		<Button href={LOGIN} variant="link" class="mt-4 w-full">
			Log in instead
		</Button>
	</Card.Content>
</Card.Root>
