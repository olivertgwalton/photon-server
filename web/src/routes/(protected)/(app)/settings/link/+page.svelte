<script lang="ts">
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import PageHeader from "#lib/components/PageHeader.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";
import { page } from "$app/state";

let { data } = $props();
let message = $state<string>();
let linked = $state.raw<components["schemas"]["Device"]>();

async function link(event: SubmitEvent) {
	const code = fields(event).get("code");
	const { data: device, error } = await client().POST(
		"/api/v1/auth/pairings/approvals",
		{ body: { user_code: String(code) } },
	);
	message = error ? problemMessage(error) : undefined;
	linked = device;
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
		<form onsubmit={link}>
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
							event.currentTarget.value = formatCode(event.currentTarget.value);
						}}
					/>
					<Field.Description>
						It may take the device a few seconds to notice.
					</Field.Description>
				</Field.Field>
				<Field.Error errors={[{ message }]} />
				{#if linked}
					<p role="status" class="text-ink text-sm">
						{linked.device}
						({linked.client}) is signed in.
					</p>
				{/if}
				<Button type="submit">Link</Button>
			</Field.Group>
		</form>
	</Card.Content>
</Card.Root>
