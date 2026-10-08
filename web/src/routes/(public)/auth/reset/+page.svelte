<script lang="ts">
import { toast } from "svelte-sonner";
import { goto } from "$app/navigation";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { problemMessage } from "#lib/api/problem.js";
import { fields } from "#lib/form.js";
import { LOGIN } from "#lib/session.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

type Asked = components["schemas"]["ResetStart"];

const api = client();
let pending = $state(false);
let message = $state<string>();
// Where the code was written, once asked for; a reader handed a code already
// goes straight to entering it.
let asked = $state<Asked>();
let haveCode = $state(false);

async function call<T>(
	request: Promise<{ data?: T; error?: unknown; response: Response }>,
) {
	pending = true;
	const answer = await request.catch(() => undefined);
	pending = false;
	if (!answer?.response.ok) {
		message = answer
			? problemMessage(answer.error)
			: "The server isn't answering.";
		return;
	}
	message = undefined;
	return answer;
}

async function ask(event: SubmitEvent) {
	const form = fields(event);
	const answer = await call(
		api.POST("/api/v1/auth/password-resets", {
			body: { name: String(form.get("name")) },
		}),
	);
	if (answer) asked = answer.data;
}

async function redeem(event: SubmitEvent) {
	const form = fields(event);
	const password = String(form.get("password"));
	if (password !== form.get("confirm")) {
		message = "The passwords don't match.";
		return;
	}
	const answer = await call(
		api.POST("/api/v1/auth/password-resets/redemptions", {
			body: { code: String(form.get("code")), password },
		}),
	);
	if (!answer) return;
	toast.success("Your password is reset. Log in with it.");
	await goto(LOGIN);
}
</script>

<svelte:head><title>Reset password · Photon</title></svelte:head>

<Card.Root class="w-full max-w-sm">
	<Card.Header>
		<Card.Title>
			<h1 class="heading text-xl">Reset your password</h1>
		</Card.Title>
		<Card.Description>
			{#if asked}
				If a profile has that name, a code to reset its password is in the log
				of {asked.node}. Ask whoever runs the server for it; it works for
				{Math.round(asked.expires_in_ms / 60_000)}
				minutes.
			{:else if haveCode}
				Enter the code from the server's log and a new password.
			{:else}
				Ask from a device on the server's own network. The code is written to
				the server's log, for whoever runs it to give you.
			{/if}
		</Card.Description>
	</Card.Header>
	<Card.Content>
		{#if asked || haveCode}
			<form onsubmit={redeem}>
				<Field.Group>
					<Field.Field>
						<Field.Label for="code">Code</Field.Label>
						<Input
							id="code"
							name="code"
							autocomplete="one-time-code"
							autocapitalize="characters"
							spellcheck="false"
							required
						/>
					</Field.Field>
					<Field.Field>
						<Field.Label for="password">New password</Field.Label>
						<Input
							id="password"
							name="password"
							type="password"
							autocomplete="new-password"
							required
						/>
						<Field.Description>
							At least 8 characters. Every device on the profile is signed out.
						</Field.Description>
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
						<Button type="submit" disabled={pending}>Reset password</Button>
					</Field.Field>
				</Field.Group>
			</form>
		{:else}
			<form onsubmit={ask}>
				<Field.Group>
					<Field.Field>
						<Field.Label for="name">Name</Field.Label>
						<Input id="name" name="name" autocomplete="username" required />
					</Field.Field>
					<Field.Error errors={[{ message }]} />
					<Field.Field>
						<Button type="submit" disabled={pending}>Get a code</Button>
						<Button
							type="button"
							variant="ghost"
							onclick={() => (haveCode = true)}
						>
							Have a code already?
						</Button>
					</Field.Field>
				</Field.Group>
			</form>
		{/if}
		<Button href={LOGIN} variant="link" class="mt-4 w-full">
			Back to log in
		</Button>
	</Card.Content>
</Card.Root>
