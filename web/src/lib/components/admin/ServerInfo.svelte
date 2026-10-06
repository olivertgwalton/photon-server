<script lang="ts">
import type { components } from "#lib/api/schema.js";
import { accelerations, bytes, relative, when } from "#lib/admin/words.js";

// How the server is set up, read-only: it is configured by its environment.
let {
	server: s,
	now,
}: { server: components["schemas"]["Server"]; now: number } = $props();

function tool(t: components["schemas"]["Tool"]) {
	return t.version ? `${t.version} (${t.path})` : "Not found";
}

function backend(b: components["schemas"]["Backend"]) {
	if (!b.reachable) return "Not reachable";
	return b.version ? `Reachable, ${b.version}` : "Reachable";
}

const rows = $derived<[string, string][]>([
	["Version", s.version],
	[
		"Running",
		`since ${when.format(new Date(s.started_at))} (${relative(s.started_at, now)})`,
	],
	["System", `${s.os} on ${s.arch}`],
	["FFmpeg", tool(s.ffmpeg)],
	["FFprobe", tool(s.ffprobe)],
	["yt-dlp", tool(s.yt_dlp)],
	[
		"Chromaprint",
		s.chromaprint
			? "Available: intros and credits are found by sound"
			: "Not available",
	],
	[
		"Encoder",
		[
			accelerations[s.encoder.acceleration],
			s.encoder.device,
			s.encoder.hevc === "allow" ? "HEVC and H.264" : "H.264 only",
		]
			.filter(Boolean)
			.join(", "),
	],
	[
		"Transcodes",
		s.transcode_limit ? `At most ${s.transcode_limit} at once` : "No limit",
	],
	[
		"Discovery",
		s.discovery === "broadcast" ? "Answers apps looking on the network" : "Off",
	],
	["Listening on", s.listen],
	["Web app at", s.public_url ?? "Where each device reaches the server"],
	["Trusted proxies", s.trusted_proxies.join(", ") || "None"],
	["Metadata language", s.metadata_language],
	["Postgres", backend(s.postgres)],
	["Valkey", backend(s.valkey)],
]);

const folders = $derived<[string, components["schemas"]["Folder"]][]>([
	["Cache", s.folders.cache],
	["Backups", s.folders.backups],
]);
</script>

<dl class="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[auto_1fr]">
	{#each rows as [name, value] (name)}
		<dt class="label pt-0.5">{name}</dt>
		<dd class="text-ink-2 min-w-0 break-words">{value}</dd>
	{/each}
	{#each folders as [name, folder] (name)}
		<dt class="label pt-0.5">{name}</dt>
		<dd class="text-ink-2 min-w-0 break-words">
			{folder.path}
			{#if folder.free_bytes != null}
				<span class="text-ink-3"
					>· {bytes.format(folder.free_bytes / 1e9)} free</span
				>
			{/if}
		</dd>
	{/each}
</dl>

{#if s.nodes.length}
	<h3 class="label mt-6 mb-2">Nodes</h3>
	<ul class="divide-line divide-y text-sm">
		{#each s.nodes as node (node.id)}
			<li class="flex flex-wrap justify-between gap-x-4 py-2">
				<span class="text-ink font-mono">
					{node.address}{node.id === s.node_id ? " (this node)" : ""}
				</span>
				<span class="text-ink-3">seen {relative(node.last_seen, now)}</span>
			</li>
		{/each}
	</ul>
{/if}
