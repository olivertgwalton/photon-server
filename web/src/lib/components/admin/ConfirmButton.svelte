<script lang="ts">
import type { Snippet } from "svelte";
import * as AlertDialog from "#lib/components/ui/alert-dialog/index.js";
import { buttonVariants } from "#lib/components/ui/button/index.js";

// A button for something that cannot be undone, which asks first.
let {
	label,
	hidden = "",
	title,
	confirm,
	onconfirm,
	children,
}: {
	label: string;
	// Said to a screen reader after the label, to tell one row's button from the next.
	hidden?: string;
	title: string;
	confirm: string;
	onconfirm: () => unknown;
	children: Snippet;
} = $props();
</script>

<AlertDialog.Root>
	<AlertDialog.Trigger
		class={buttonVariants({ variant: "outline", size: "sm" })}
	>
		{label}
		{#if hidden}
			<span class="sr-only">{hidden}</span>
		{/if}
	</AlertDialog.Trigger>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{title}</AlertDialog.Title>
			<AlertDialog.Description>{@render children()}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				class={buttonVariants({ variant: "destructive" })}
				onclick={onconfirm}
			>
				{confirm}
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
