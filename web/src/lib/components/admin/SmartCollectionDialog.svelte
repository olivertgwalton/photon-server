<script lang="ts">
import { goto } from "$app/navigation";
import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

// Keeps a library's filtered wall as a smart collection, as Plex's "Create
// Smart Collection" from a filtered library: its titles are what the filter
// finds, found again as the library changes.
let {
	open = $bindable(false),
	library,
	rule,
}: {
	open?: boolean;
	library: string;
	rule: components["schemas"]["SmartRule"];
} = $props();

let title = $state("");

async function make(event: SubmitEvent) {
	event.preventDefault();
	const { data, error } = await client().POST("/api/v1/admin/collections", {
		body: { library_id: library, title, rule },
	});
	if (error) return toast.error(problemMessage(error));
	open = false;
	toast.success(`${title} is a smart collection.`);
	await goto(`/settings/server/collections/${data.id}`);
}
</script>

<Dialog.Root bind:open>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Save as a smart collection</Dialog.Title>
			<Dialog.Description>
				It holds what these filters find, in this order, kept up to date as the
				library changes.
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={make} class="grid gap-4">
			<Field.Field>
				<Field.Label for="smart-title">Name</Field.Label>
				<Input
					id="smart-title"
					bind:value={title}
					required
					placeholder="Recent comedies"
				/>
			</Field.Field>
			<Dialog.Footer>
				<Button type="submit">Save</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
