<script lang="ts">
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

// Keeps a library's filtered wall as a smart playlist, as Plex's "Create Smart
// Playlist" from a filtered library: it plays what the filters find, found
// again whenever it is opened, so one of unwatched episodes loses an episode as
// it is watched.
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

let name = $state("");

async function make(event: SubmitEvent) {
	event.preventDefault();
	const saved = await act(
		client().POST("/api/v1/playlists", {
			body: { name, library_id: library, rule },
		}),
		`${name} plays what these filters find.`,
		(made) => `/playlists/${made.id}`,
	);
	if (saved) open = false;
}
</script>

<Dialog.Root bind:open>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Save as a smart playlist</Dialog.Title>
			<Dialog.Description>
				It plays what these filters find, in this order, worked out again each
				time you open it.
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={make} class="grid gap-4">
			<Field.Field>
				<Field.Label for="{id}-smart-name">Name</Field.Label>
				<Input
					id="{id}-smart-name"
					bind:value={name}
					required
					placeholder="Not yet seen"
				/>
			</Field.Field>
			<Dialog.Footer>
				<Button type="submit">Save</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
