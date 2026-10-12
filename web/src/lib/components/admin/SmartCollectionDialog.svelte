<script lang="ts">
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
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

const id = $props.id();

let title = $state("");

async function make(event: SubmitEvent) {
	event.preventDefault();
	const saved = await act(
		client().POST("/api/v1/admin/collections", {
			body: { library_id: library, title, rule },
		}),
		`${title} is a smart collection.`,
		(made) => `/settings/server/collections/${made.id}`,
	);
	if (saved) open = false;
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
				<Field.Label for="{id}-smart-title">Name</Field.Label>
				<Input
					id="{id}-smart-title"
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
