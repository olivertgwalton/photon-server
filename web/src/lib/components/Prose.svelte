<script lang="ts">
import * as Dialog from "#lib/components/ui/dialog/index.js";
import { onResize } from "#lib/size.js";

// Writing held to a few lines, as the app's synopses and biographies are, with
// Read more opening the whole of it over the page rather than pushing the page
// down. It shows only where there is more. `title` names what it is about.
let {
	text,
	lines,
	title,
	class: className,
}: { text: string; lines: number; title: string; class?: string } = $props();

let clamped = $state(false);
const measure = onResize<HTMLParagraphElement>((p) => {
	clamped = p.scrollHeight > p.clientHeight + 1;
});
</script>

<div class={className}>
	<p
		{@attach measure}
		class="line-clamp-(--lines) leading-relaxed whitespace-pre-line"
		style="--lines: {lines}"
	>
		{text}
	</p>
	{#if clamped}
		<Dialog.Root>
			<Dialog.Trigger
				class="text-ink mt-1 text-xs font-bold tracking-wide uppercase hover:underline"
			>
				Read more
			</Dialog.Trigger>
			<Dialog.Content class="max-h-[85svh] overflow-y-auto sm:max-w-2xl">
				<Dialog.Header>
					<Dialog.Title>{title}</Dialog.Title>
				</Dialog.Header>
				<p class="text-ink-2 leading-relaxed whitespace-pre-line">{text}</p>
			</Dialog.Content>
		</Dialog.Root>
	{/if}
</div>
