<script lang="ts">
import ShieldAlertIcon from "@lucide/svelte/icons/shield-alert";
import { relative } from "#lib/admin/words.ts";
import { client } from "#lib/api/client.ts";
import type { components } from "#lib/api/schema.d.ts";
import PageHeader from "#lib/components/PageHeader.svelte";
import * as Alert from "#lib/components/ui/alert/index.ts";
import { Button } from "#lib/components/ui/button/index.ts";
import * as Card from "#lib/components/ui/card/index.ts";
import * as Field from "#lib/components/ui/field/index.ts";
import { Input } from "#lib/components/ui/input/index.ts";
import { fields } from "#lib/form.ts";
import { attempt } from "#lib/signin.ts";
import { page } from "$app/state";

let { data } = $props();
let message = $state<string>();
let pending = $state(false);
let waiting = $state.raw<components["schemas"]["WaitingPairing"]>();
let linked = $state.raw<components["schemas"]["Device"]>();

// A code is looked at before it is approved, so a link someone else sent shows whose device it
// would sign in, and from where it asked.
async function find(event: SubmitEvent) {
	const code = fields(event).get("code");
	linked = undefined;
	pending = true;
	const settled = await attempt(
		client().POST("/api/v1/auth/pairings/find", {
			body: { user_code: String(code) },
		}),
	);
	pending = false;
	message = settled.message;
	waiting = settled.answer?.data;
}

async function approve() {
	if (!waiting) return;
	pending = true;
	const settled = await attempt(
		client().POST("/api/v1/auth/pairings/approvals", {
			body: { user_code: waiting.user_code },
		}),
	);
	pending = false;
	message = settled.message;
	linked = settled.answer?.data;
	waiting = undefined;
}

// A letter code reads XXXX-XXXX, as the television shows it; Jellyfin's six digits have no dash.
function formatCode(typed: string) {
	const code = typed.toUpperCase().replace(/[^A-Z0-9]/g, "");
	return /[A-Z]/.test(code) && code.length > 4
		? `${code.slice(0, 4)}-${code.slice(4, 8)}`
		: code;
}
</script>

<PageHeader
	title="Link a device"
	description={`Enter the code your TV or other device is showing to sign it in as ${data.me.name}.`}
/>

<Card.Root class="max-w-sm">
	<Card.Content>
		{#if waiting}
			<Field.Group>
				<p class="text-ink text-sm">
					<span class="font-medium">{waiting.device}</span>
					({waiting.client}) asked
					{relative(waiting.asked_at, Date.now())}, from
					{waiting.network === "local"
						? "your network"
						: "outside your network"}.
				</p>
				<!-- RFC 8628 3.3.1: a code that arrived in a link is checked against the device's own. -->
				<p class="text-ink text-sm">
					Sign it in only if the device in front of you shows
					<span class="font-mono text-base font-medium"
						>{waiting.user_code}</span
					>.
				</p>
				{#if waiting.network === "remote"}
					<Alert.Root variant="destructive">
						<ShieldAlertIcon />
						<Alert.Title>This device isn't on your network</Alert.Title>
						<Alert.Description>
							If someone sent you this link, signing in gives them your account.
						</Alert.Description>
					</Alert.Root>
				{/if}
				<Field.Error errors={[{ message }]} />
				<div class="flex gap-2">
					<Button onclick={approve} disabled={pending}>
						Sign it in as {data.me.name}
					</Button>
					<Button
						variant="outline"
						disabled={pending}
						onclick={() => {
							waiting = undefined;
							message = undefined;
						}}
					>
						Cancel
					</Button>
				</div>
			</Field.Group>
		{:else}
			<form onsubmit={find}>
				<Field.Group>
					<Field.Field>
						<Field.Label for="code">Code</Field.Label>
						<Input
							id="code"
							name="code"
							required
							autocomplete="off"
							autocapitalize="characters"
							spellcheck="false"
							placeholder="XXXX-XXXX"
							class="font-mono text-lg tracking-[0.2em] uppercase"
							value={formatCode(page.url.searchParams.get("code") ?? "")}
							oninput={(event) => {
								event.currentTarget.value = formatCode(
									event.currentTarget.value,
								);
							}}
						/>
						<Field.Description>
							You'll see which device asked before it's signed in.
						</Field.Description>
					</Field.Field>
					<Field.Error errors={[{ message }]} />
					{#if linked}
						<p role="status" class="text-ink text-sm">
							{linked.device}
							({linked.client}) is signed in. It may take it a few seconds to
							notice.
						</p>
					{/if}
					<Button type="submit" disabled={pending}>Continue</Button>
				</Field.Group>
			</form>
		{/if}
	</Card.Content>
</Card.Root>
