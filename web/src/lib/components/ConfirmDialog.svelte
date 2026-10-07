<script lang="ts">
import { confirming } from "#lib/actions.svelte.js";
import * as AlertDialog from "#lib/components/ui/alert-dialog/index.js";
import { buttonVariants } from "#lib/components/ui/button/index.js";
</script>

<AlertDialog.Root bind:open={confirming.open}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{confirming.title}</AlertDialog.Title>
			<AlertDialog.Description>{confirming.body}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				class={buttonVariants({ variant: "destructive" })}
				onclick={() => confirming.run()}
			>
				{confirming.act}
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
