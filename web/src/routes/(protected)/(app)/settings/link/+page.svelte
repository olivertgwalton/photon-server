<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { page } from "$app/state";
import type { components } from "#lib/api/schema.js";
import { client, problemMessage } from "#lib/api/client.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { fields } from "#lib/form.js";

let { data } = $props();
let message = $state<string>();
let linked = $state<components["schemas"]["Device"]>();

async function link(event: SubmitEvent) {
	const code = fields(event).get("code");
	const { data: device, error } = await client().POST(
		"/api/v1/auth/device/approve",
		{ body: { user_code: String(code) } },
	);
	message = error ? problemMessage(error) : undefined;
	linked = device;
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
						value={page.url.searchParams.get("code") ?? ""}
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
