<script lang="ts">
import { backAfterRestore } from "#lib/restored.js";
import { restoring } from "#lib/restoring.svelte.js";

// Over everything while the server restores its database, until a node
// answers again; the page is then loaded afresh, as what it showed may be gone.
$effect(() => {
	if (!restoring.on) return;
	let gone = false;
	backAfterRestore().then(() => {
		if (!gone) location.reload();
	});
	return () => {
		gone = true;
	};
});
</script>

{#if restoring.on}
	<div
		role="alertdialog"
		aria-modal="true"
		aria-labelledby="restoring-title"
		aria-describedby="restoring-said"
		class="bg-ground fixed inset-0 z-200 grid place-items-center px-4"
	>
		<div class="grid max-w-md gap-2 text-center">
			<h1 id="restoring-title" class="title">Restoring…</h1>
			<p id="restoring-said" class="text-ink-2 text-sm">
				The server is restoring its database and will be back shortly. This page
				reloads once it is.
			</p>
		</div>
	</div>
{/if}
