<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { act } from "#lib/act.js";
import { fields } from "#lib/form.js";
import { ticking } from "#lib/admin/clock.svelte.js";
import { relative, roleOptions, roles } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import Choice from "#lib/components/admin/Choice.svelte";
import ProfileAvatar from "#lib/components/ProfileAvatar.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import * as Table from "#lib/components/ui/table/index.js";

let { data } = $props();

const locks = { pin: "PIN", password: "Password" } as const;
// A manager adds users.
const options = $derived(roleOptions(data.me.role));

let adding = $state(false);
let role = $state<keyof typeof roles>("user");
const clock = ticking(30_000);

async function add(event: SubmitEvent) {
	const form = fields(event);
	const name = String(form.get("name") ?? "");
	const password = String(form.get("password") ?? "");
	const added = await act(
		client().POST("/api/v1/admin/profiles", {
			body: { name, role, password },
		}),
		`${name} was added.`,
	);
	if (added) adding = false;
}
</script>

<PageHeader
	title="Profiles"
	description="Who watches here. Each keeps their own progress, favourites and playlists; an admin also decides what each may see."
>
	{#snippet actions()}
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
							It signs in with its password, and is switched to with it until it
							sets a PIN.
						</Dialog.Description>
					</Dialog.Header>
					<Field.Group>
						<Field.Field>
							<Field.Label for="profile-name">Name</Field.Label>
							<Input
								id="profile-name"
								name="name"
								required
								autocomplete="off"
							/>
						</Field.Field>
						{#if options.length > 1}
							<Field.Field>
								<Field.Label for="profile-role">Role</Field.Label>
								<Choice
									id="profile-role"
									name="role"
									bind:value={role}
									{options}
								/>
								<Field.Description>
									A user sees only what its access allows, set once it is added.
									A manager adds users and keeps the ones it added.
								</Field.Description>
							</Field.Field>
						{/if}
						<Field.Field>
							<Field.Label for="profile-password">Password</Field.Label>
							<Input
								id="profile-password"
								name="password"
								type="password"
								autocomplete="new-password"
								minlength={8}
								required
							/>
						</Field.Field>
					</Field.Group>
					<Dialog.Footer>
						<Button type="submit">Add profile</Button>
					</Dialog.Footer>
				</form>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</PageHeader>

<Table.Root>
	<Table.Header>
		<Table.Row>
			<Table.Head>Profile</Table.Head>
			<Table.Head>Role</Table.Head>
			<Table.Head>Lock</Table.Head>
			{#if data.me.role === "admin"}
				<Table.Head>Last seen</Table.Head>
			{/if}
		</Table.Row>
	</Table.Header>
	<Table.Body>
		{#each data.profiles as profile (profile.id)}
			<Table.Row>
				<Table.Cell>
					<a
						href="/settings/profiles/{profile.id}"
						class="text-ink flex items-center gap-3 font-semibold hover:underline"
					>
						<ProfileAvatar
							name={profile.name}
							avatar={profile.avatar}
							class="size-8 text-sm"
						/>
						{profile.name}
					</a>
				</Table.Cell>
				<Table.Cell>{roles[profile.role]}</Table.Cell>
				<Table.Cell>{locks[profile.lock]}</Table.Cell>
				{#if data.me.role === "admin"}
					<Table.Cell
						>{profile.seen
							? relative(profile.seen, clock.now)
							: "Never"}</Table.Cell
					>
				{/if}
			</Table.Row>
		{/each}
	</Table.Body>
</Table.Root>
