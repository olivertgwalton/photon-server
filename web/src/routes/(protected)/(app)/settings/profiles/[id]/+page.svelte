<script lang="ts">
import { vocabulary } from "#lib/vocabulary.js";
import { act } from "#lib/act.js";
import { fields } from "#lib/form.js";
import { accessOf } from "#lib/admin/access.js";
import { roleOptions } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import AccessFields from "#lib/components/admin/AccessFields.svelte";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import AvatarPicker from "#lib/components/AvatarPicker.svelte";
import ProfileAvatar from "#lib/components/ProfileAvatar.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

type Change = components["schemas"]["ProfileChange"];

let { data } = $props();
const words = vocabulary();

const api = client();
const path = $derived({ params: { path: { id: data.profile.id } } });

// A manager keeps users.
const options = $derived(roleOptions(data.me.role, words.roles));

const locks = {
	pin: "Switched to with a PIN its owner set.",
	password: "Switched to with its password.",
} as const;

async function saveProfile(event: SubmitEvent) {
	const form = fields(event);
	const body: Change = {
		name: String(form.get("name") ?? ""),
		role: String(form.get("role")) as Change["role"],
	};
	const password = String(form.get("password") ?? "");
	if (password) body.password = password;
	if (
		await act(
			api.PATCH("/api/v1/admin/profiles/{id}", { ...path, body }),
			password ? "Saved, with the new password." : "Saved.",
		)
	) {
		(event.currentTarget as HTMLFormElement).reset();
	}
}

function saveAccess(event: SubmitEvent) {
	return act(
		api.PUT("/api/v1/admin/profiles/{id}/access", {
			...path,
			body: accessOf(fields(event)),
		}),
		"What this profile sees was saved.",
	);
}

function remove() {
	return act(
		api.DELETE("/api/v1/admin/profiles/{id}", path),
		`${data.profile.name} was removed.`,
		"/settings/profiles",
	);
}
</script>

<svelte:head
	><title>{data.profile.name} · Settings · Photon</title></svelte:head
>

<div class="grid max-w-2xl gap-6">
	<header class="flex items-center gap-4">
		<ProfileAvatar
			name={data.profile.name}
			avatar={data.profile.avatar}
			class="size-16 text-2xl"
		/>
		<div class="grid gap-2">
			<h1 class="title">{data.profile.name}</h1>
			<AvatarPicker
				name={data.profile.name}
				avatar={data.profile.avatar}
				path="/api/v1/admin/profiles/{data.profile.id}/avatar"
			/>
		</div>
	</header>

	<Card.Root>
		<Card.Header>
			<Card.Title><h2 class="heading">Profile</h2></Card.Title>
			<Card.Description>{locks[data.profile.lock]}</Card.Description>
		</Card.Header>
		<Card.Content>
			<form onsubmit={saveProfile}>
				<Field.Group>
					<Field.Field>
						<Field.Label for="name">Name</Field.Label>
						<Input id="name" name="name" required value={data.profile.name} />
					</Field.Field>
					<Field.Field>
						<Field.Label for="role">Role</Field.Label>
						<Choice id="role" name="role" value={data.profile.role} {options} />
						<Field.Description>
							The last admin cannot stop being one.
						</Field.Description>
					</Field.Field>
					<Field.Field>
						<Field.Label for="password"> New password </Field.Label>
						<Input
							id="password"
							name="password"
							type="password"
							autocomplete="new-password"
							minlength={8}
						/>
						<Field.Description>
							Leave it empty to keep the password as it is. A PIN is set by the
							profile's owner, in their settings.
						</Field.Description>
					</Field.Field>
					<Field.Field orientation="horizontal">
						<Button type="submit">Save</Button>
					</Field.Field>
				</Field.Group>
			</form>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title><h2 class="heading">What it sees</h2></Card.Title>
			<Card.Description>
				The libraries it may open, and the oldest certificate it may watch.
			</Card.Description>
		</Card.Header>
		<Card.Content>
			<form onsubmit={saveAccess}>
				<Field.Group>
					<AccessFields access={data.access} libraries={data.libraries} />
					<Button type="submit" class="justify-self-start">Save access</Button>
				</Field.Group>
			</form>
		</Card.Content>
	</Card.Root>

	<div class="flex items-center gap-3">
		<ConfirmButton
			onconfirm={remove}
			label="Remove this profile"
			title="Remove {data.profile.name}?"
			confirm="Remove profile"
			body={"Its devices are signed out, and what it has watched is forgotten. The last admin cannot be removed."}
		/>
		<Button href="/settings/profiles" variant="ghost">Back to profiles</Button>
	</div>
</div>
