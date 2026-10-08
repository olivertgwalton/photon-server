<script lang="ts">
import type { Component, Snippet } from "svelte";
import IconButton from "#lib/components/IconButton.svelte";
import * as AlertDialog from "#lib/components/ui/alert-dialog/index.js";
import { buttonVariants } from "#lib/components/ui/button/index.js";

// A button for something that cannot be undone, which asks first; with an
// icon, shown as it.
let {
	label,
	hidden = "",
	icon,
	title,
	confirm,
	onconfirm,
	children,
}: {
	label: string;
	// Said to a screen reader after the label, to tell one row's button from the next.
	hidden?: string;
	icon?: Component;
	title: string;
	confirm: string;
	onconfirm: () => unknown;
	children: Snippet;
} = $props();
</script>

<AlertDialog.Root>
	{#if icon}
		<AlertDialog.Trigger>
			{#snippet child({
				props,
			})}
				<IconButton {...props} {label} {hidden} {icon} tone="destructive" />
			{/snippet}
		</AlertDialog.Trigger>
	{:else}
		<AlertDialog.Trigger
			class={buttonVariants({ variant: "outline", size: "sm" })}
		>
			{label}
			{#if hidden}
				<span class="sr-only">{hidden}</span>
			{/if}
		</AlertDialog.Trigger>
	{/if}
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
