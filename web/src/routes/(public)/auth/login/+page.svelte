<script lang="ts">
import { enhance } from "$app/forms";
import { page } from "$app/state";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

let { data, form } = $props();
let pending = $state(false);

// The action keeps the page's query, so a failed attempt still knows where to
// return to.
const action = $derived.by(() => {
	const to = page.url.searchParams.get("to");
	return to ? `?/login&to=${encodeURIComponent(to)}` : "?/login";
});
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
		<form
			method="post"
			{action}
			use:enhance={() => {
				pending = true;
				return async ({ update }) => {
					await update();
					pending = false;
				};
			}}
		>
			<Field.Group>
				<Field.Field>
					<Field.Label for="name">Name</Field.Label>
					<Input
						id="name"
						name="name"
						autocomplete="username"
						required
						value={form?.name ?? ""}
					/>
				</Field.Field>
				<Field.Field>
					<Field.Label for="password">Password</Field.Label>
					<Input
						id="password"
						name="password"
						type="password"
						autocomplete="current-password"
					/>
				</Field.Field>
				<Field.Error errors={[{ message: form?.message }]} />
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
