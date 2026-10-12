<script lang="ts">
import * as Dialog from "#lib/components/ui/dialog/index.js";
import { Input } from "#lib/components/ui/input/index.js";
import { getShell } from "#lib/shell.svelte.js";

const shell = getShell();
</script>

<Dialog.Root bind:open={shell.sharing.open}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Share {shell.sharing.title}</Dialog.Title>
			<Dialog.Description>Copy its link to pass it on.</Dialog.Description>
		</Dialog.Header>
		<Input
			readonly
			value={shell.sharing.url}
			aria-label="Link"
			onfocus={(e) => e.currentTarget.select()}
		/>
	</Dialog.Content>
</Dialog.Root>
