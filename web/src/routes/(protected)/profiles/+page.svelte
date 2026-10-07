<script lang="ts">
import LockIcon from "@lucide/svelte/icons/lock";
import { goto } from "$app/navigation";
import { client, problemMessage } from "#lib/api/client.js";
import ProfileAvatar from "#lib/components/ProfileAvatar.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";

let { data } = $props();
let message = $state<string>();

// Watches as the profile, with its PIN or password unless it is the one watched
// as now, then goes back to where the reader was.
async function choose(event: SubmitEvent) {
	const form = fields(event);
	const profileID = String(form.get("profile_id"));
	const secret = form.get("secret");
	const { error } = await client().PUT("/api/v1/session/profile", {
		body: secret
			? { profile_id: profileID, secret: String(secret) }
			: { profile_id: profileID },
	});
	message = error ? problemMessage(error) : undefined;
	if (!error) await goto(data.to, { invalidateAll: true });
}

const avatar =
	"size-24 text-4xl sm:size-32 sm:text-5xl group-hover:**:data-[slot=avatar-fallback]:bg-ink group-hover:**:data-[slot=avatar-fallback]:text-ground group-focus-visible:**:data-[slot=avatar-fallback]:bg-ink group-focus-visible:**:data-[slot=avatar-fallback]:text-ground";
// The profile this browser is watching as now.
const currentRing = "ring-ink ring-offset-ground ring-2 ring-offset-4";
const target = "group grid justify-items-center gap-3 rounded-xl outline-none";
</script>

<svelte:head><title>Who's watching? · Photon</title></svelte:head>

<main class="grid min-h-svh place-items-center p-6">
	{#if data.chosen}
		<form
			class="grid w-full max-w-xs justify-items-center gap-4"
			onsubmit={choose}
		>
			<ProfileAvatar
				name={data.chosen.name}
				avatar={data.chosen.avatar}
				class="size-24 text-4xl"
			/>
			<h1 class="title">{data.chosen.name}</h1>
			<input type="hidden" name="profile_id" value={data.chosen.id}>
			<Field.Group>
				<Field.Field>
					<Field.Label for="secret">
						{data.chosen.lock === "pin" ? "PIN" : "Password"}
					</Field.Label>
					{#if data.chosen.lock === "pin"}
						<Input
							id="secret"
							name="secret"
							type="password"
							inputmode="numeric"
							autocomplete="off"
							pattern="[0-9]*"
							minlength={4}
							maxlength={6}
							required
						/>
					{:else}
						<Input
							id="secret"
							name="secret"
							type="password"
							autocomplete="current-password"
							required
						/>
					{/if}
				</Field.Field>
				<Field.Error errors={[{ message }]} />
				<Field.Field orientation="horizontal" class="justify-center">
					<Button
						variant="ghost"
						href="/profiles?to={encodeURIComponent(data.to)}"
					>
						Back
					</Button>
					<Button type="submit">Continue</Button>
				</Field.Field>
			</Field.Group>
		</form>
	{:else}
		<div class="grid justify-items-center gap-10">
			<h1 class="title">Who's watching?</h1>
			<ul class="flex flex-wrap justify-center gap-6 sm:gap-8">
				{#each data.profiles as profile (profile.id)}
					{@const current = profile.id === data.current}
					<li>
						{#if current}
							<form onsubmit={choose}>
								<input type="hidden" name="profile_id" value={profile.id}>
								<button
									type="submit"
									class={target}
									aria-current={current ? "true" : undefined}
								>
									<ProfileAvatar
										name={profile.name}
										avatar={profile.avatar}
										class="{avatar} {current ? currentRing : ""}"
									/>
									<span class="text-ink-2 group-hover:text-ink font-semibold">
										{profile.name}
									</span>
								</button>
							</form>
						{:else}
							<a
								class={target}
								href="?profile={profile.id}&to={encodeURIComponent(data.to)}"
							>
								<ProfileAvatar
									name={profile.name}
									avatar={profile.avatar}
									class={avatar}
								/>
								<span
									class="text-ink-2 group-hover:text-ink flex items-center gap-1.5 font-semibold"
								>
									{profile.name}
									<LockIcon class="size-3.5" aria-label="Locked" />
								</span>
							</a>
						{/if}
					</li>
				{/each}
			</ul>
			<Field.Error errors={[{ message }]} />
		</div>
	{/if}
</main>
