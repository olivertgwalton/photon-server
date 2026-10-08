<script lang="ts">
import { act } from "#lib/act.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { fields } from "#lib/form.js";
import { runtime } from "#lib/format.js";

// Asks the server for a copy to keep, as Plex's Download asks: the file as it
// is, or converted down to a size for a phone or a slow connection. A copy
// already within the choice comes back as itself. A copy in several files is
// kept a file at a time, all of them unless one is chosen: Plex's Save File and
// Jellyfin's Download give its first file alone.
let {
	open = $bindable(false),
	id,
	title,
	version,
}: {
	open?: boolean;
	id: string;
	title: string;
	version?: components["schemas"]["VersionPage"];
} = $props();

// The most the picture may be. Original is any size at all.
const qualities = [
	{ name: "Original", detail: "The file as it is", kbps: 1_000_000 },
	{ name: "1080p", detail: "20 Mbps", kbps: 20_000, width: 1920 },
	{ name: "1080p", detail: "8 Mbps", kbps: 8000, width: 1920 },
	{ name: "720p", detail: "4 Mbps", kbps: 4000, width: 1280 },
	{ name: "480p", detail: "1.5 Mbps", kbps: 1500, width: 854 },
];

const files = $derived(version?.files ?? []);
// Which of its files: "all", or one's id.
const fileChoices = $derived([
	{ id: "all", label: `All ${files.length} files` },
	...files.map((f) => ({
		id: f.id,
		label: `Part ${f.index + 1} · ${runtime(f.duration_ms)}`,
	})),
]);

async function request(event: SubmitEvent) {
	const form = fields(event);
	const { kbps, width } = qualities[Number(form.get("quality"))];
	const file = form.get("file") ?? "all";
	const parts =
		files.length > 1
			? files.filter((f) => file === "all" || f.id === file)
			: [undefined];
	const asked = await Promise.all(
		parts.map((part) =>
			client().POST("/api/v1/downloads", {
				body: {
					title_id: id,
					version_id: version?.id,
					part_id: part?.id,
					max_bitrate_kbps: kbps,
					max_width: width,
				},
			}),
		),
	);
	const said =
		parts.length > 1
			? `${parts.length} downloads requested, one a file. They're in Downloads.`
			: "Download requested. It's in Downloads.";
	if (
		await act(Promise.resolve(asked.find((a) => a.error) ?? asked[0]), said)
	) {
		open = false;
	}
}
</script>

<Dialog.Root bind:open>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>Download</Dialog.Title>
			<Dialog.Description>{title}</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={request} class="grid gap-4">
			{#if files.length > 1}
				<Field.Set>
					<Field.Legend>Files</Field.Legend>
					<div class="grid gap-1">
						{#each fileChoices as option (option.id)}
							<label
								class="hover:bg-accent has-checked:bg-accent flex items-center gap-3 rounded-md px-3 py-2"
							>
								<input
									type="radio"
									name="file"
									value={option.id}
									checked={option.id === "all"}
									class="accent-ink"
								>
								<span class="text-ink font-semibold">{option.label}</span>
							</label>
						{/each}
					</div>
				</Field.Set>
			{/if}
			<Field.Set>
				<Field.Legend>Quality</Field.Legend>
				<div class="grid gap-1">
					{#each qualities as quality, i (i)}
						<label
							class="hover:bg-accent has-checked:bg-accent flex items-center gap-3 rounded-md px-3 py-2"
						>
							<input
								type="radio"
								name="quality"
								value={i}
								checked={i === 0}
								class="accent-ink"
							>
							<span class="text-ink font-semibold">{quality.name}</span>
							<span class="text-ink-3 text-sm">{quality.detail}</span>
						</label>
					{/each}
				</div>
			</Field.Set>
			<Dialog.Footer>
				<Button type="submit">Download</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
