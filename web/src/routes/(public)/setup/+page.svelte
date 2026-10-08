<script lang="ts">
import { goto } from "$app/navigation";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import { CLIENT, deviceName } from "#lib/device.js";
import { fields } from "#lib/form.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

let { data } = $props();
let pending = $state(false);
let message = $state<string>();

async function setUp(event: SubmitEvent) {
	const form = fields(event);
	const password = String(form.get("password"));
	// The only admin's password is typed twice: no one is left to reset it.
	if (password !== form.get("confirm")) {
		message = "The passwords don't match.";
		return;
	}
	pending = true;
	const { data: signedIn, error } = await client()
		.POST("/api/v1/setup", {
			body: {
				name: String(form.get("name")),
				password,
				device: deviceName(navigator.userAgent),
				client: CLIENT,
				keep: "cookie",
			},
		})
		.catch(() => ({ data: undefined, error: undefined }));
	if (!signedIn) {
		pending = false;
		message = error ? problemMessage(error) : "The server isn't answering.";
		return;
	}
	// Signed in as the admin, the rest of setup is the admin's to do.
	await goto("/setup/server", { refreshAll: true });
}
</script>

<svelte:head><title>Set up · Photon</title></svelte:head>

<Card.Root class="w-full max-w-sm">
	<Card.Header>
		<Card.Title>
			<h1 class="heading text-xl">Set up {data.server.name}</h1>
		</Card.Title>
		<Card.Description>
			{data.state === "open"
				? "Create the admin profile. It runs the server and adds everyone else."
				: "A new server is set up from its own network, at its own address rather than through a proxy. Open this page on a device at home, or on the server itself."}
		</Card.Description>
	</Card.Header>
	{#if data.state === "open"}
		<Card.Content>
			<form onsubmit={setUp}>
				<Field.Group>
					<Field.Field>
						<Field.Label for="name">Name</Field.Label>
						<Input id="name" name="name" autocomplete="username" required />
					</Field.Field>
					<Field.Field>
						<Field.Label for="password">Password</Field.Label>
						<Input
							id="password"
							name="password"
							type="password"
							autocomplete="new-password"
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
						<Button type="submit" disabled={pending}>Create admin</Button>
					</Field.Field>
				</Field.Group>
			</form>
		</Card.Content>
	{/if}
</Card.Root>
