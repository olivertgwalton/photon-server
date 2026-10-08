<script lang="ts">
import { goto } from "$app/navigation";
import { page } from "$app/state";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import { CLIENT, deviceName } from "#lib/device.js";
import { returnPath } from "#lib/session.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";

let { data } = $props();
let pending = $state(false);
let message = $state<string>();

async function login(event: SubmitEvent) {
	const form = fields(event);
	pending = true;
	const api = client();
	const { data: signedIn, error } = await api
		.POST("/api/v1/auth/login", {
			body: {
				method: "password",
				name: String(form.get("name")),
				password: String(form.get("password")),
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
	// A household of several is asked who is watching, as Plex and Netflix
	// ask; a household of one goes straight on.
	const to = returnPath(page.url);
	const profiles = await api.GET("/api/v1/profiles");
	await goto(
		(profiles.data?.items.length ?? 0) > 1
			? `/profiles?to=${encodeURIComponent(to)}`
			: to,
		{ refreshAll: true },
	);
}
</script>

<svelte:head><title>Log in · Photon</title></svelte:head>

<Card.Root class="w-full max-w-sm">
	<Card.Header>
		<Card.Title>
			<h1 class="heading text-xl">
				{data.server ? `Log in to ${data.server.name}` : "Log in"}
			</h1>
		</Card.Title>
		<Card.Description>
			{data.server
				? "Use your profile's name and password."
				: "The server isn't answering. You can log in once it's back."}
		</Card.Description>
	</Card.Header>
	<Card.Content>
		<form onsubmit={login}>
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
						autocomplete="current-password"
					/>
					<Field.Description>
						<a href="/auth/reset">Forgot your password?</a>
					</Field.Description>
				</Field.Field>
				<Field.Error errors={[{ message }]} />
				<Field.Field>
					<Button type="submit" disabled={pending}>Log in</Button>
					<Field.Description class="text-center">
						Setting up a TV? Choose to sign in with a code on it, then enter the
						code at <span class="text-ink font-mono">/link</span> here once
						you're logged in.
					</Field.Description>
				</Field.Field>
			</Field.Group>
		</form>
	</Card.Content>
</Card.Root>
