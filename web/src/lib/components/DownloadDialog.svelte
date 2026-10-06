<script lang="ts">
import { change } from "#lib/actions.svelte.js";
import { client } from "#lib/api/client.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";

// Asks the server for a copy to keep, as Plex's Download asks: the file as it
// is, or converted down to a size for a phone or a slow connection. A copy
// already within the choice comes back as itself.
let {
	open = $bindable(false),
	id,
	title,
	version,
}: { open?: boolean; id: string; title: string; version?: string } = $props();

// The most the picture may be. Original is any size at all.
const qualities = [
	{ name: "Original", detail: "The file as it is", kbps: 1_000_000 },
	{ name: "1080p", detail: "20 Mbps", kbps: 20_000, width: 1920 },
	{ name: "1080p", detail: "8 Mbps", kbps: 8000, width: 1920 },
	{ name: "720p", detail: "4 Mbps", kbps: 4000, width: 1280 },
	{ name: "480p", detail: "1.5 Mbps", kbps: 1500, width: 854 },
];
let chosen = $state(0);

async function request(event: SubmitEvent) {
	event.preventDefault();
	const { kbps, width } = qualities[chosen];
	const asked = client().POST("/api/v1/downloads", {
		body: {
			title_id: id,
			version_id: version,
			max_bitrate_kbps: kbps,
			max_width: width,
		},
	});
	if (await change(asked, "Download requested. It's in Downloads.")) {
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
		<form onsubmit={request}>
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
								bind:group={chosen}
								class="accent-ink"
							>
							<span class="text-ink font-semibold">{quality.name}</span>
							<span class="text-ink-3 text-sm">{quality.detail}</span>
						</label>
					{/each}
				</div>
			</Field.Set>
			<Dialog.Footer class="mt-4">
				<Button type="submit">Download</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
