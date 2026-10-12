<script lang="ts">
import * as AlertDialog from "#lib/components/ui/alert-dialog/index.js";
import { buttonVariants } from "#lib/components/ui/button/index.js";
import { getShell } from "#lib/shell.svelte.js";
import { cn } from "#lib/utils.js";

const shell = getShell();
</script>

<!-- bits-ui's Action does not close its dialog: the answer does, then acts. -->
<AlertDialog.Root bind:open={shell.confirming.open}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{shell.confirming.title}</AlertDialog.Title>
			<AlertDialog.Description>{shell.confirming.body}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				class={cn(buttonVariants({ variant: "destructive" }))}
				onclick={() => {
					shell.confirming.open = false;
					return shell.confirming.run();
				}}
			>
				{shell.confirming.act}
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
