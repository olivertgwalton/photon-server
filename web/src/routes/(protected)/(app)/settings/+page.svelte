<script lang="ts">
import { vocabulary } from "#lib/vocabulary.js";
import { toast } from "svelte-sonner";
import { refreshAll } from "$app/navigation";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import AvatarPicker from "#lib/components/AvatarPicker.svelte";
import ProfileAvatar from "#lib/components/ProfileAvatar.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";

let { data } = $props();
const words = vocabulary();
// Why a change was refused, beside the card it was made in.
let refused = $state<{ name?: string; pin?: string; password?: string }>({});

// A change that worked says so in a toast and redraws the page; one refused
// says why beside it.
async function change(
	call: Promise<{ error?: unknown }>,
	card: keyof typeof refused,
	said: string,
) {
	const { error } = await call;
	refused = error ? { [card]: problemMessage(error) } : {};
	if (error) return false;
	toast.success(said);
	await refreshAll();
	return true;
}

function rename(event: SubmitEvent) {
	const name = String(fields(event).get("name"));
	return change(
		client().PATCH("/api/v1/me", { body: { name } }),
		"name",
		"Renamed, on every device.",
	);
}

async function setPIN(event: SubmitEvent) {
	const form = event.currentTarget as HTMLFormElement;
	const pin = String(fields(event).get("pin"));
	if (
		await change(
			client().PUT("/api/v1/me/pin", { body: { pin } }),
			"pin",
			"PIN set.",
		)
	) {
		form.reset();
	}
}

const clearPIN = () =>
	change(client().DELETE("/api/v1/me/pin"), "pin", "PIN removed.");

async function setPassword(event: SubmitEvent) {
	const form = event.currentTarget as HTMLFormElement;
	const values = fields(event);
	const next = String(values.get("new"));
	if (next !== values.get("again")) {
		refused = { password: "The new password and its repeat differ." };
		return;
	}
	const asked = client().PUT("/api/v1/me/password", {
		body: { current: String(values.get("current")), new: next },
	});
	if (
		await change(
			asked,
			"password",
			"Password changed. Your other devices are signed out.",
		)
	) {
		form.reset();
	}
}
</script>

<svelte:head><title>Profile · Settings · Photon</title></svelte:head>

<div class="grid max-w-2xl gap-6">
	<header class="flex items-center gap-4">
		<ProfileAvatar
			name={data.me.name}
			avatar={data.me.avatar}
			class="size-16 text-2xl"
		/>
		<div class="grid gap-2">
			<h1 class="title">{data.me.name}</h1>
			<p class="text-ink-2 text-sm">
				{words.roles[data.me.role]}.
				{#if data.me.role === "admin" || data.me.role === "manager"}
					What each profile may see is changed under
					<a href="/settings/profiles" class="text-ink underline">Profiles</a>.
				{:else}
					An admin changes what this profile may see.
				{/if}
			</p>
			<AvatarPicker
				name={data.me.name}
				avatar={data.me.avatar}
				path="/api/v1/me/avatar"
			/>
		</div>
	</header>

	<Card.Root>
		<Card.Header>
			<Card.Title><h2 class="heading">Name</h2></Card.Title>
			<Card.Description>
				What every device calls you. No two profiles share a name.
			</Card.Description>
		</Card.Header>
		<Card.Content>
			<form onsubmit={rename}>
				<Field.Group>
					<Field.Field data-invalid={refused.name ? true : undefined}>
						<Field.Label for="name">Name</Field.Label>
						<Input
							id="name"
							name="name"
							required
							maxlength={64}
							class="max-w-sm"
							autocomplete="nickname"
							value={data.me.name}
							aria-invalid={refused.name ? true : undefined}
						/>
						<Field.Error errors={[{ message: refused.name }]} />
					</Field.Field>
					<Field.Field orientation="horizontal">
						<Button type="submit">Rename</Button>
					</Field.Field>
				</Field.Group>
			</form>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title><h2 class="heading">Password</h2></Card.Title>
			<Card.Description>
				Changing it signs out every other device signed in as you.
			</Card.Description>
		</Card.Header>
		<Card.Content>
			<form onsubmit={setPassword}>
				<Field.Group>
					<Field.Field>
						<Field.Label for="current">Current password</Field.Label>
						<Input
							id="current"
							name="current"
							type="password"
							autocomplete="current-password"
							class="max-w-sm"
							required
						/>
					</Field.Field>
					<Field.Field>
						<Field.Label for="new">New password</Field.Label>
						<Input
							id="new"
							name="new"
							type="password"
							autocomplete="new-password"
							class="max-w-sm"
							required
						/>
					</Field.Field>
					<Field.Field>
						<Field.Label for="again">Repeat the new password</Field.Label>
						<Input
							id="again"
							name="again"
							type="password"
							autocomplete="new-password"
							class="max-w-sm"
							required
						/>
					</Field.Field>
					<Field.Error errors={[{ message: refused.password }]} />
					<Field.Field orientation="horizontal">
						<Button type="submit">Change password</Button>
					</Field.Field>
				</Field.Group>
			</form>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title><h2 class="heading">PIN</h2></Card.Title>
			<Card.Description>
				{#if data.me.role === "admin"}
					An admin's profile is always opened with its password, so it has no
					PIN.
				{:else if data.lock === "pin"}
					Switching to this profile asks for its PIN.
				{:else}
					Switching to this profile asks for its password. Set a PIN of 4 to 6
					digits to ask for that instead.
				{/if}
			</Card.Description>
		</Card.Header>
		{#if data.me.role !== "admin"}
			<Card.Content>
				<form onsubmit={setPIN}>
					<Field.Group>
						<Field.Field>
							<Field.Label for="new-pin">
								{data.lock === "pin" ? "New PIN" : "PIN"}
							</Field.Label>
							<Input
								id="new-pin"
								name="pin"
								type="password"
								inputmode="numeric"
								autocomplete="off"
								pattern="[0-9]{"{"}4,6{"}"}"
								minlength={4}
								maxlength={6}
								required
								class="max-w-36"
							/>
						</Field.Field>
						<Field.Error errors={[{ message: refused.pin }]} />
						<Field.Field orientation="horizontal">
							<Button type="submit">
								{data.lock === "pin" ? "Change PIN" : "Set PIN"}
							</Button>
							{#if data.lock === "pin"}
								<Button type="button" variant="outline" onclick={clearPIN}>
									Remove PIN
								</Button>
							{/if}
						</Field.Field>
					</Field.Group>
				</form>
			</Card.Content>
		{/if}
	</Card.Root>
</div>
