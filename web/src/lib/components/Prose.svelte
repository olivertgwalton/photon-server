<script lang="ts">
import * as Dialog from "#lib/components/ui/dialog/index.js";

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
let paragraph = $state<HTMLParagraphElement>();

$effect(() => {
	const p = paragraph;
	if (!p) return;
	const watch = new ResizeObserver(() => {
		clamped = p.scrollHeight > p.clientHeight + 1;
	});
	watch.observe(p);
	return () => watch.disconnect();
});
</script>

<div class={className}>
	<p
		bind:this={paragraph}
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
