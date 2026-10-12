<script lang="ts">
import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
import type { Attachment } from "svelte/attachments";
import { titleWithShow } from "#lib/format.js";
import { getShell, type MenuCard } from "#lib/shell.svelte.js";
import { cn } from "#lib/utils.js";

// Opens the shell's one title menu beside it.
let { card, class: className }: { card: MenuCard; class?: string } = $props();
const shell = getShell();

let button = $state<HTMLButtonElement>();
const open = $derived(
	shell.titleMenu.open && shell.titleMenu.anchor === button,
);

function toggle(anchor: HTMLButtonElement) {
	if (open) shell.titleMenu.open = false;
	else shell.openTitleMenu(card, anchor);
}

// A card scrolled out of a wall takes its menu with it.
const closesMenu: Attachment<HTMLButtonElement> = (element) => () => {
	if (shell.titleMenu.anchor === element) shell.titleMenu.open = false;
};
</script>

<button
	bind:this={button}
	{@attach closesMenu}
	type="button"
	class={cn(
		"bg-ground/90 text-ink hover:bg-ink hover:text-ground grid size-8 place-items-center rounded-full",
		className,
	)}
	aria-label="More for {titleWithShow(card)}"
	aria-haspopup="menu"
	aria-expanded={open}
	data-state={open ? "open" : "closed"}
	onclick={(e) => toggle(e.currentTarget)}
	onkeydown={(e) => {
		if (e.key !== "ArrowDown" || open) return;
		e.preventDefault();
		shell.openTitleMenu(card, e.currentTarget);
	}}
>
	<EllipsisIcon class="size-4" />
</button>
