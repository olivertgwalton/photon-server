<script lang="ts">
import { act, fields } from "#lib/admin/act.js";
import { relative, roles } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import Choice from "#lib/components/admin/Choice.svelte";
import ProfileAvatar from "#lib/components/ProfileAvatar.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import * as Table from "#lib/components/ui/table/index.js";

let { data } = $props();

const locks = { none: "None", pin: "PIN", password: "Password" } as const;
const roleOptions = Object.entries(roles).map(([value, label]) => ({
	value: value as keyof typeof roles,
	label,
}));

let adding = $state(false);
let role = $state<keyof typeof roles>("member");
const now = Date.now();

async function add(event: SubmitEvent) {
	const form = fields(event);
	const name = String(form.get("name") ?? "");
	const password = String(form.get("password") ?? "");
	const added = await act(
		client().POST("/api/v1/admin/profiles", {
			body: { name, role, ...(password ? { password } : {}) },
		}),
		`${name} was added.`,
	);
	if (added) adding = false;
}
</script>

<svelte:head><title>Profiles · Dashboard · Photon</title></svelte:head>

<div class="flex flex-wrap items-center justify-between gap-4">
	<h1 class="title">Profiles</h1>
	<Dialog.Root bind:open={adding}>
		<Dialog.Trigger>
			{#snippet child({
				props,
			})}
				<Button {...props}>Add a profile</Button>
			{/snippet}
		</Dialog.Trigger>
		<Dialog.Content>
			<form onsubmit={add} class="grid gap-6">
				<Dialog.Header>
					<Dialog.Title>Add a profile</Dialog.Title>
					<Dialog.Description>
						A profile with no password is chosen on a device someone has already
						signed in to, as a household's are. An admin always has one.
					</Dialog.Description>
				</Dialog.Header>
				<Field.Group>
					<Field.Field>
						<Field.Label for="profile-name">Name</Field.Label>
						<Input id="profile-name" name="name" required autocomplete="off" />
					</Field.Field>
					<Field.Field>
						<Field.Label for="profile-role">Role</Field.Label>
						<Choice
							id="profile-role"
							name="role"
							bind:value={role}
							options={roleOptions}
						/>
						<Field.Description>
							A restricted profile sees only what its access allows.
						</Field.Description>
					</Field.Field>
					<Field.Field>
						<Field.Label for="profile-password">Password</Field.Label>
						<Input
							id="profile-password"
							name="password"
							type="password"
							autocomplete="new-password"
							required={role === "admin"}
						/>
					</Field.Field>
				</Field.Group>
				<Dialog.Footer>
					<Button type="submit">Add profile</Button>
				</Dialog.Footer>
			</form>
		</Dialog.Content>
	</Dialog.Root>
</div>

<Table.Root>
	<Table.Header>
		<Table.Row>
			<Table.Head>Profile</Table.Head>
			<Table.Head>Role</Table.Head>
			<Table.Head>Lock</Table.Head>
			<Table.Head>Last seen</Table.Head>
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each data.profiles as profile (profile.id)}
			<Table.Row>
				<Table.Cell>
					<a
						href="/settings/server/profiles/{profile.id}"
						class="text-ink flex items-center gap-3 font-semibold hover:underline"
					>
						<ProfileAvatar name={profile.name} class="size-8 text-sm" />
						{profile.name}
					</a>
				</Table.Cell>
				<Table.Cell>{roles[profile.role]}</Table.Cell>
				<Table.Cell>{locks[profile.lock]}</Table.Cell>
				<Table.Cell
					>{profile.seen ? relative(profile.seen, now) : "Never"}</Table.Cell
				>
			</Table.Row>
		{/each}
	</Table.Body>
</Table.Root>
