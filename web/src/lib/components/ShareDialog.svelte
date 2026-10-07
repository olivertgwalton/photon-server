<script lang="ts">
import { sharing } from "#lib/actions.svelte.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import { Input } from "#lib/components/ui/input/index.js";
</script>

<Dialog.Root bind:open={sharing.open}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Share {sharing.title}</Dialog.Title>
			<Dialog.Description>Copy its link to pass it on.</Dialog.Description>
		</Dialog.Header>
		<Input
			readonly
			value={sharing.url}
			aria-label="Link"
			onfocus={(e) => e.currentTarget.select()}
		/>
	</Dialog.Content>
</Dialog.Root>
