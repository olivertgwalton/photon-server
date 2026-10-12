<script lang="ts">
import type { Attachment } from "svelte/attachments";
import { backAfterRestore } from "#lib/restored.js";
import { restoring } from "#lib/restoring.svelte.js";
import { page } from "$app/state";

const id = $props.id();

// Over everything while the server restores its database, until a node
// answers again; the page is then loaded afresh, as what it showed may be gone.
const reloadsWhenBack: Attachment = () => {
	let gone = false;
	backAfterRestore().then(() => {
		if (!gone) location.reload();
	});
	return () => {
		gone = true;
	};
};
</script>

<!-- A restore the backups page loaded is shown as one told live is. -->
{#if restoring.on || page.data.backups?.restoring}
	<div
		{@attach reloadsWhenBack}
		role="alertdialog"
		aria-modal="true"
		aria-labelledby="{id}-restoring-title"
		aria-describedby="{id}-restoring-said"
		class="bg-ground fixed inset-0 z-200 grid place-items-center px-4"
	>
		<div class="grid max-w-md gap-2 text-center">
			<h1 id="{id}-restoring-title" class="title">Restoring…</h1>
			<p id="{id}-restoring-said" class="text-ink-2 text-sm">
				The server is restoring its database and will be back shortly. This page
				reloads once it is.
			</p>
		</div>
	</div>
{/if}
