<script lang="ts">
import { Button } from "#lib/components/ui/button/index.ts";
import * as Dialog from "#lib/components/ui/dialog/index.ts";
import * as Field from "#lib/components/ui/field/index.ts";
import { Input } from "#lib/components/ui/input/index.ts";

// Names a library's filtered wall to keep, as Plex's "Create Smart Collection"
// and "Create Smart Playlist" from a filtered library: what it is kept as, and
// where, is the page's. It closes once save says it worked.
let {
	open = $bindable(false),
	heading,
	about,
	placeholder,
	save,
}: {
	open?: boolean;
	heading: string;
	about: string;
	placeholder: string;
	save: (name: string) => Promise<unknown>;
} = $props();

const id = $props.id();

let name = $state("");

async function submit(event: SubmitEvent) {
	event.preventDefault();
	if (await save(name)) open = false;
}
</script>

<Dialog.Root bind:open>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>{heading}</Dialog.Title>
			<Dialog.Description>{about}</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={submit} class="grid gap-4">
			<Field.Field>
				<Field.Label for="{id}-smart-name">Name</Field.Label>
				<Input id="{id}-smart-name" bind:value={name} required {placeholder} />
			</Field.Field>
			<Dialog.Footer>
				<Button type="submit">Save</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
