<script lang="ts">
import { mergeProps } from "bits-ui";
import type { Component } from "svelte";
import { Button, type ButtonProps } from "#lib/components/ui/button/index.js";
import * as Tooltip from "#lib/components/ui/tooltip/index.js";

// A button shown as its icon, named by a tooltip; the props of a dialog's or
// popover's trigger may be spread on it. A busy one spins its icon.
let {
	label,
	hidden = "",
	icon: Icon,
	tone = "plain",
	...rest
}: ButtonProps & {
	label: string;
	// Said to a screen reader after the label, to tell one row's button from the next.
	hidden?: string;
	icon: Component;
	tone?: "plain" | "destructive";
} = $props();
</script>

<Tooltip.Root>
	<Tooltip.Trigger>
		{#snippet child({
			props,
		})}
			<Button
				{...mergeProps(rest, props)}
				variant="ghost"
				size="icon-sm"
				aria-label={hidden ? `${label} ${hidden}` : label}
				class={[
					"aria-busy:[&_svg]:animate-spin",
					tone === "destructive"
						? "text-destructive hover:bg-destructive/10 hover:text-destructive"
						: "text-ink-2",
				]}
			>
				<Icon aria-hidden="true" />
			</Button>
		{/snippet}
	</Tooltip.Trigger>
	<Tooltip.Content>{label}</Tooltip.Content>
</Tooltip.Root>
