<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { act } from "#lib/act.js";
import { when } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import { bytes } from "#lib/format.js";
import { Button } from "#lib/components/ui/button/index.js";

let { data } = $props();

const b = $derived(data.backups);
</script>

<PageHeader
	title="Backups"
	description="The server dumps its database every three days, keeping the newest three, on whichever node runs its scheduled tasks. These are the dumps this node keeps; another node may keep others."
>
	{#snippet actions()}
		<Button
			variant="outline"
			onclick={() =>
				act(
					client().POST("/api/v1/admin/tasks/{key}/run", {
						params: { path: { key: "backup_database" } },
					}),
					"The database is being backed up.",
				)}
		>
			Back up now
		</Button>
	{/snippet}
</PageHeader>

<section aria-labelledby="dumps" class="grid gap-3">
	<h2 id="dumps" class="heading">On {b.node_name}</h2>
	<p class="text-ink-3 text-sm [overflow-wrap:anywhere]">In {b.folder}</p>
	{#if b.items.length}
		<ul class="grid gap-3">
			{#each b.items as dump (dump.name)}
				<li
					class="bg-raise grid gap-3 rounded-xl p-4 sm:grid-cols-[1fr_auto] sm:items-center"
				>
					<div class="grid min-w-0 gap-1">
						<p class="text-ink truncate font-semibold">
							{when.format(new Date(dump.made_at))}
						</p>
						<p class="text-ink-3 truncate text-xs">
							{dump.name}
							· {bytes(dump.size_bytes)}
						</p>
					</div>
					<Button
						href={`/api/v1/admin/backups/${encodeURIComponent(dump.name)}`}
						download={dump.name}
						variant="outline"
						size="sm"
						>Download</Button
					>
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
		A dump is restored from the command line, with every node stopped: the
		restore refuses while anything is connected to the database, and refuses a
		dump made by a newer version of the server.
	</p>
	<pre class="bg-raise overflow-x-auto rounded-xl p-4 text-sm"><code
			>photon-server restore {b.folder}/{b.items[0]?.name ?? "photon-…dump"}</code
		></pre>
	<p class="text-ink-2 text-sm">
		It replaces the database with the dump's, brings it up to this version, and
		forgets what Valkey held of playbacks, nodes and pairings. Cached artwork
		and previews are left; each node's sweeps forget what the database no longer
		has. Start the nodes again once it is done.
	</p>
</section>
