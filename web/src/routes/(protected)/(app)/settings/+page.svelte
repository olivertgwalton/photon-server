<script lang="ts">
import { toast } from "svelte-sonner";
import { enhance, type SubmitFunction } from "$app/forms";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

let { data, form } = $props();

const when = new Intl.DateTimeFormat(undefined, {
	dateStyle: "medium",
	timeStyle: "short",
});

// A change that worked says so in a toast; one refused says why beside it.
const enhanced: SubmitFunction =
	({ formElement }) =>
	async ({ result, update }) => {
		await update();
		if (result.type === "success" && typeof result.data?.said === "string") {
			toast.success(result.data.said);
			formElement.reset();
		}
	};
</script>

<svelte:head><title>Settings · Photon</title></svelte:head>

<div class="mx-auto grid max-w-2xl gap-6">
	<h1 class="title">Settings</h1>

	<Card.Root>
		<Card.Header>
			<Card.Title><h2 class="heading">PIN</h2></Card.Title>
			<Card.Description>
				{#if data.lock === "password"}
					An admin's profile is always opened with its password, so it has no
					PIN.
				{:else if data.lock === "pin"}
					Switching to this profile asks for its PIN.
				{:else}
					Anyone signed in to this household can switch to this profile. Set a
					PIN of 4 to 6 digits to lock it.
				{/if}
			</Card.Description>
		</Card.Header>
		{#if data.lock !== "password"}
			<Card.Content>
				<form method="post" action="?/setPIN" use:enhance={enhanced}>
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
						<Field.Error errors={[{ message: form?.pin }]} />
						<Field.Field orientation="horizontal">
							<Button type="submit">
								{data.lock === "pin" ? "Change PIN" : "Set PIN"}
							</Button>
							{#if data.lock === "pin"}
								<Button
									type="submit"
									variant="outline"
									formaction="?/clearPIN"
									formnovalidate
								>
									Remove PIN
								</Button>
							{/if}
						</Field.Field>
					</Field.Group>
				</form>
			</Card.Content>
		{/if}
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title><h2 class="heading">Signed-in devices</h2></Card.Title>
			<Card.Description>
				Everything signed in to this household. Signing a device out ends its
				session at once.
			</Card.Description>
		</Card.Header>
		<Card.Content>
			<ul class="divide-line divide-y">
				{#each data.devices as device (device.id)}
					<li class="flex flex-wrap items-center gap-x-4 gap-y-2 py-3">
						<div class="min-w-0 flex-1">
							<p class="text-ink font-semibold">
								{device.device}
								{#if device.this_device}
									<span class="label ml-2">This browser</span>
								{/if}
							</p>
							<p class="text-ink-3 text-sm">
								{device.client}
								· {device.profile} · last seen
								{when.format(new Date(device.last_seen_at))}
							</p>
						</div>
						<form
							method="post"
							action={device.this_device
								? "/auth/login?/logout"
								: "?/signOutDevice"}
							use:enhance={device.this_device ? undefined : enhanced}
						>
							<input type="hidden" name="id" value={device.id}>
							<Button
								type="submit"
								variant="outline"
								size="sm"
								aria-label="Sign out {device.device}"
							>
								Sign out
							</Button>
						</form>
					</li>
				{/each}
			</ul>
			<Field.Error errors={[{ message: form?.devices }]} />
		</Card.Content>
	</Card.Root>
</div>
