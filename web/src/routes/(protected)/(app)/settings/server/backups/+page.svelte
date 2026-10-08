<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { runTask } from "#lib/actions.svelte.js";
import { act } from "#lib/act.js";
import { when } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import { bytes } from "#lib/format.js";
import { restoring } from "#lib/restoring.svelte.js";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import { Button } from "#lib/components/ui/button/index.js";

let { data } = $props();

const b = $derived(data.backups);
const last = $derived(b.last_restore);

// A restore under way, as the page loaded it, is shown as one told live is.
$effect(() => {
	if (b.restoring) restoring.on = true;
});

async function restore(name: string) {
	const ok = await act(
		client().POST("/api/v1/admin/backups/{name}/restore", {
			params: { path: { name } },
		}),
	);
	if (ok) restoring.on = true;
}
</script>

<PageHeader
	title="Backups"
	description="The server dumps its database every three days, keeping the newest three, on whichever node runs its scheduled tasks. These are the dumps this node keeps; another node may keep others."
>
	{#snippet actions()}
		<Button
			variant="outline"
			onclick={() =>
				runTask("backup_database", "The database is being backed up.")}
		>
			Back up now
		</Button>
	{/snippet}
</PageHeader>

{#if last}
	<p
		role="status"
		class={[
			"rounded-xl border p-4 text-sm",
			last.result === "failed" ? "border-destructive/40" : "border-line",
		]}
	>
		{#if last.result === "succeeded"}
			The last restore, of {last.dump}, succeeded
			{when.format(new Date(last.at))}.
		{:else}
			The last restore, of {last.dump}, failed
			{when.format(new Date(last.at))}: {last.reason}
		{/if}
	</p>
{/if}

<section aria-labelledby="dumps" class="grid gap-3">
	<h2 id="dumps" class="heading">On {b.node_name}</h2>
	<p class="text-ink-3 text-sm [overflow-wrap:anywhere]">In {b.folder}</p>
	{#if b.items.length}
		<ul class="grid gap-3">
			{#each b.items as dump (dump.name)}
				{@const made = when.format(new Date(dump.made_at))}
				<li
					class="bg-raise grid gap-3 rounded-xl p-4 sm:grid-cols-[1fr_auto] sm:items-center"
				>
					<div class="grid min-w-0 gap-1">
						<p class="text-ink truncate font-semibold">{made}</p>
						<p class="text-ink-3 truncate text-xs">
							{dump.name}
							· {bytes(dump.size_bytes)}
						</p>
					</div>
					<div class="flex flex-wrap gap-2">
						<Button
							href={`/api/v1/admin/backups/${encodeURIComponent(dump.name)}`}
							download={dump.name}
							variant="outline"
							size="sm"
							>Download</Button
						>
						<ConfirmButton
							label="Restore"
							hidden={made}
							title="Restore this backup?"
							confirm="Restore and restart"
							onconfirm={() => restore(dump.name)}
						>
							The database goes back to {dump.name}, made {made}: whatever
							changed since is lost. Every stream stops, and every server stops
							and starts again, which takes a minute or two.
						</ConfirmButton>
					</div>
				</li>
			{/each}
		</ul>
	{:else}
		<p class="text-ink-3 text-sm">This node keeps no backups yet.</p>
	{/if}
</section>

<section aria-labelledby="restoring" class="grid max-w-2xl gap-3">
	<h2 id="restoring" class="heading">Restoring</h2>
	<p class="text-ink-2 text-sm">
		Restoring here stops every server and restores the dump on this one. The
		servers start again on their own under <code>restart: unless-stopped</code>,
		Docker's <code>on-failure</code> or systemd's
		<code>Restart=on-failure</code>; otherwise, start them again by hand. A dump
		made by a newer version of the server is refused.
	</p>
	<p class="text-ink-2 text-sm">
		Where a server cannot start, a dump is restored from the command line, with
		every server stopped:
	</p>
	<pre class="bg-raise overflow-x-auto rounded-xl p-4 text-sm"><code
			>photon-server restore {b.folder}/{b.items[0]?.name ?? "photon-…dump"}</code
		></pre>
	<p class="text-ink-2 text-sm">
		Either way, the database is replaced with the dump's in one transaction and
		brought up to this version, and what Valkey held of playbacks, nodes and
		pairings is forgotten. Cached artwork and previews are left; each server's
		sweeps forget what the database no longer has.
	</p>
</section>
