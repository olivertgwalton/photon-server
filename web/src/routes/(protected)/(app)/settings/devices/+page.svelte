<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { client } from "#lib/api/client.js";
import { act } from "#lib/act.js";
import { logOut } from "#lib/logout.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";

let { data } = $props();

const when = new Intl.DateTimeFormat(undefined, {
	dateStyle: "medium",
	timeStyle: "short",
});

const signOut = (id: string) =>
	act(
		client().DELETE("/api/v1/auth/devices/{id}", { params: { path: { id } } }),
		"That device is signed out.",
	);
</script>

<PageHeader
	title="Devices"
	description={`Everything signed in to this household. Signing a device out ends its session at once; to sign in a television, use Link a device.`}
/>

<Card.Root>
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
					<Button
						variant="outline"
						size="sm"
						aria-label="Sign out {device.device}"
						onclick={() => (device.this_device ? logOut() : signOut(device.id))}
					>
						Sign out
					</Button>
				</li>
			{/each}
		</ul>
	</Card.Content>
</Card.Root>
