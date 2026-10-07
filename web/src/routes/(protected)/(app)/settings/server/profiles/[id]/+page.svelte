<script lang="ts">
import { act } from "#lib/admin/act.js";
import { fields } from "#lib/form.js";
import { roles } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/admin/Choice.svelte";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import AvatarPicker from "#lib/components/AvatarPicker.svelte";
import ProfileAvatar from "#lib/components/ProfileAvatar.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import { Checkbox } from "#lib/components/ui/checkbox/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { Label } from "#lib/components/ui/label/index.js";
import { Switch } from "#lib/components/ui/switch/index.js";

type Change = components["schemas"]["ProfileChange"];

let { data } = $props();

const api = client();
const path = $derived({ params: { path: { id: data.profile.id } } });

const roleOptions = Object.entries(roles).map(([value, label]) => ({
	value: value as keyof typeof roles,
	label,
}));

// The ages certificates are for, as the server reads them.
const ages = [
	{ value: "any", label: "Any" },
	...[0, 6, 7, 9, 12, 13, 15, 16, 17, 18].map((age) => ({
		value: String(age),
		label: age ? `Up to ${age}` : "Suitable for all",
	})),
];
const age = $derived(
	data.access.max_age == null ? "any" : String(data.access.max_age),
);
const ageOptions = $derived(
	ages.some((a) => a.value === age)
		? ages
		: [...ages, { value: age, label: `Up to ${age}` }],
);

let every = $state(false);
$effect.pre(() => {
	every = data.access.libraries.length === 0;
});

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
	const form = fields(event);
	const asked = String(form.get("max_age") ?? "any");
	return act(
		api.PUT("/api/v1/admin/profiles/{id}/access", {
			...path,
			body: {
				max_age: asked === "any" ? null : Number(asked),
				unrated: form.get("unrated") === "block" ? "block" : "allow",
				libraries: every ? [] : form.getAll("libraries").map(String),
			},
		}),
		"What this profile sees was saved.",
	);
}

function remove() {
	return act(
		api.DELETE("/api/v1/admin/profiles/{id}", path),
		`${data.profile.name} was removed.`,
		"/settings/server/profiles",
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
						<Choice
							id="role"
							name="role"
							value={data.profile.role}
							options={roleOptions}
						/>
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
					<Field.Set>
						<Field.Legend>Libraries</Field.Legend>
						<div class="flex items-center gap-2">
							<Switch id="every" bind:checked={every} />
							<Label for="every"
								>Every library, including ones added later</Label
							>
						</div>
						{#if !every}
							<div class="grid gap-2 sm:grid-cols-2">
								{#each data.libraries as library (library.id)}
									<div class="flex items-center gap-2">
										<Checkbox
											id="library-{library.id}"
											name="libraries"
											value={library.id}
											checked={data.access.libraries.includes(library.id)}
										/>
										<Label for="library-{library.id}">{library.name}</Label>
									</div>
								{/each}
							</div>
						{/if}
					</Field.Set>
					<div class="grid gap-4 sm:grid-cols-2">
						<Field.Field>
							<Field.Label for="max_age">Certificates</Field.Label>
							<Choice
								id="max_age"
								name="max_age"
								value={age}
								options={ageOptions}
							/>
						</Field.Field>
						<Field.Field>
							<Field.Label for="unrated"
								>Titles with no certificate</Field.Label
							>
							<Choice
								id="unrated"
								name="unrated"
								value={data.access.unrated}
								options={[
									{ value: "allow", label: "Shown" },
									{ value: "block", label: "Hidden" },
								]}
							/>
						</Field.Field>
					</div>
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
		>
			Its devices are signed out, and what it has watched is forgotten. The last
			admin cannot be removed.
		</ConfirmButton>
		<Button href="/settings/server/profiles" variant="ghost"
			>Back to profiles</Button
		>
	</div>
</div>
