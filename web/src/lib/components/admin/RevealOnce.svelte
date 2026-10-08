<script lang="ts">
import type { Snippet } from "svelte";
import { toast } from "svelte-sonner";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import { Input } from "#lib/components/ui/input/index.js";

// A secret the server answers once, when it is made: shown to be copied, and
// never again. It is open while `value` is set.
let {
	value,
	title,
	label,
	copied,
	onclose,
	children,
}: {
	value: string | undefined;
	title: string;
	label: string;
	copied: string;
	onclose: () => void;
	children: Snippet;
} = $props();

async function copy() {
	if (!value) return;
	await navigator.clipboard.writeText(value);
	toast.success(copied);
}
</script>

<Dialog.Root
	open={!!value}
	onOpenChange={(open) => {
		if (!open) onclose();
	}}
>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>{title}</Dialog.Title>
			<Dialog.Description>{@render children()}</Dialog.Description>
		</Dialog.Header>
		<div class="flex gap-2">
			<Input
				value={value ?? ""}
				readonly
				aria-label={label}
				class="font-mono"
			/>
			<Button onclick={copy}>Copy</Button>
		</div>
	</Dialog.Content>
</Dialog.Root>
