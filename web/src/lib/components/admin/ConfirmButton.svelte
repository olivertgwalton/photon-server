<script lang="ts">
import { Button } from "#lib/components/ui/button/index.ts";
import { getShell } from "#lib/shell.svelte.ts";

// A button for something that cannot be undone, which asks first.
let {
	label,
	hidden = "",
	title,
	body,
	confirm,
	onconfirm,
}: {
	label: string;
	// Said to a screen reader after the label, to tell one row's button from the next.
	hidden?: string;
	title: string;
	body: string;
	confirm: string;
	onconfirm: () => unknown;
} = $props();
const shell = getShell();
</script>

<Button
	variant="outline"
	size="sm"
	onclick={() => shell.confirmFirst(title, body, confirm, onconfirm)}
>
	{label}
	{#if hidden}
		<span class="sr-only">{hidden}</span>
	{/if}
</Button>
