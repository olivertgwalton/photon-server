<script lang="ts">
import type { components } from "#lib/api/schema.js";
import {
	accelerations,
	bytes,
	limitSources,
	relative,
	transcodeLoad,
	when,
} from "#lib/admin/words.js";
import * as Table from "#lib/components/ui/table/index.js";

// How the server is set up, read-only: it is configured by its environment.
let {
	server: s,
	now,
}: { server: components["schemas"]["Server"]; now: number } = $props();

function tool(t: components["schemas"]["Tool"]) {
	return t.version ? `${t.version} (${t.path})` : "Not found";
}

// What a node encodes with, in a few words.
function encodes(e: components["schemas"]["Node"]["encoder"]) {
	return [
		accelerations[e.acceleration],
		e.hevc === "allow" ? "H.264, HEVC" : "H.264",
		e.libass ? "subtitles" : "",
	]
		.filter(Boolean)
		.join(" · ");
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
		"libass",
		s.libass
			? "Available: styled subtitles are drawn into video for clients that cannot draw them"
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
		`${transcodeLoad(s.transcodes, s.transcode_limit)} at once · ${limitSources[s.transcode_limit_source]}`,
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
	<Table.Root class="mt-6 text-sm">
		<caption class="label mb-2 text-left">
			Nodes
		</caption>
		<Table.Header>
			<Table.Row>
				<Table.Head class="whitespace-normal">Node</Table.Head>
				<Table.Head class="whitespace-normal">Transcodes with</Table.Head>
				<Table.Head class="whitespace-normal">Transcoding</Table.Head>
			</Table.Row>
		</Table.Header>
		<Table.Body>
			{#each s.nodes as node (node.id)}
				<Table.Row>
					<Table.Cell class="whitespace-normal">
						<span class="text-ink">
							{node.name || node.address}{node.id === s.node_id
								? " (this node)"
								: ""}
						</span>
						<span class="text-ink-3 block text-xs break-all">
							{#if node.name}
								{node.address}
								·
							{/if}
							seen {relative(node.last_seen, now)}
						</span>
					</Table.Cell>
					<Table.Cell class="whitespace-normal"
						>{encodes(node.encoder)}</Table.Cell
					>
					<Table.Cell class="whitespace-normal">
						{transcodeLoad(node.transcodes, node.transcode_limit)}
						{#if node.conversions}
							<span class="text-ink-3 block text-xs"
								>{node.conversions}
								for downloads</span
							>
						{/if}
					</Table.Cell>
				</Table.Row>
			{/each}
		</Table.Body>
	</Table.Root>
{/if}
