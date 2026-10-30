<script lang="ts">
import { toast } from "svelte-sonner";
import { act } from "#lib/act.ts";
import { accessOf } from "#lib/admin/access.ts";
import { Clock } from "#lib/admin/clock.ts";
import { relative, roleOptions } from "#lib/admin/words.ts";
import { client } from "#lib/api/client.ts";
import { problemMessage } from "#lib/api/problem.ts";
import type { components } from "#lib/api/schema.d.ts";
import AccessFields from "#lib/components/admin/AccessFields.svelte";
import Choice from "#lib/components/Choice.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";
import ProfileAvatar from "#lib/components/ProfileAvatar.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import * as Dialog from "#lib/components/ui/dialog/index.ts";
import * as Field from "#lib/components/ui/field/index.ts";
import { Input } from "#lib/components/ui/input/index.ts";
import * as Table from "#lib/components/ui/table/index.ts";
import { fields, textAsTyped } from "#lib/form.ts";
import { vocabulary } from "#lib/vocabulary.ts";
import { goto } from "$app/navigation";

let { data } = $props();
const words = vocabulary();

const locks = { pin: "PIN", password: "Password" } as const;
// A manager adds users.
const options = $derived(roleOptions(data.me.role, words.roles));

let adding = $state(false);
let role = $state<components["schemas"]["Role"]>("user");
const clock = new Clock(30_000);

async function add(event: SubmitEvent) {
	const form = fields(event);
	const name = textAsTyped(form, "name");
	const password = textAsTyped(form, "password");
	const api = client();
	const added = await api.POST("/api/v1/admin/profiles", {
		body: { name, role, password },
	});
	if (added.error) {
		toast.error(problemMessage(added.error));
		return;
	}
	adding = false;
	const id = added.data.id;
	// A manager's starts seeing what the manager sees, and it cannot read that.
	const limited =
		data.me.role === "admin"
			? api.PUT("/api/v1/admin/profiles/{id}/access", {
					params: { path: { id } },
					body: accessOf(form),
				})
			: Promise.resolve({});
	// Added but seeing everything: its own page is where to narrow that.
	if (!(await act(limited, `${name} was added.`))) {
		await goto(`/settings/profiles/${id}`);
	}
}
</script>

<PageHeader
	title="Profiles"
	description="Who watches here. Each keeps their own progress, favourites and playlists; an admin also decides what each may see."
>
	{#snippet actions()}
		<Dialog.Root bind:open={adding}>
			<Dialog.Trigger>
				{#snippet child({ props })}
					<Button {...props}>Add a profile</Button>
				{/snippet}
			</Dialog.Trigger>
			<Dialog.Content class="max-h-[90dvh] overflow-y-auto">
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
									A user sees only what its access allows. A manager adds users
									and keeps the ones it added.
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
						{#if data.me.role === "admin"}
							<AccessFields
								access={{
									libraries: [],
									max_age: null,
									unrated: "allow",
									max_common_sense_age: null,
								}}
								libraries={data.libraries}
							/>
						{/if}
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
				<Table.Cell>{words.roles[profile.role]}</Table.Cell>
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
