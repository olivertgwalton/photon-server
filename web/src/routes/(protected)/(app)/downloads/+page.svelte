<script lang="ts">
import DownloadIcon from "@lucide/svelte/icons/download";
import XIcon from "@lucide/svelte/icons/x";
import { invalidate } from "$app/navigation";
import { change } from "#lib/actions.svelte.js";
import { client } from "#lib/api/client.js";
import { Button } from "#lib/components/ui/button/index.js";
import { Progress } from "#lib/components/ui/progress/index.js";
import { bitrate, bytes } from "#lib/format.js";

let { data } = $props();

function remove(id: string) {
	change(
		client().DELETE("/api/v1/downloads/{id}", { params: { path: { id } } }),
		"Download removed.",
	);
}

const states = {
	queued: "Waiting",
	converting: "Converting",
	ready: "Ready",
	failed: "Failed",
};

// A conversion says how far it has got only when asked, so the page asks
// while one is under way.
const working = $derived(
	data.downloads.some((d) => d.state === "queued" || d.state === "converting"),
);
$effect(() => {
	if (!working) return;
	const timer = setInterval(() => invalidate("photon:downloads"), 3000);
	return () => clearInterval(timer);
});
</script>

<svelte:head><title>Downloads · Photon</title></svelte:head>

<div class="grid max-w-4xl gap-8">
	<h1 class="title">Downloads</h1>
	{#if data.downloads.length}
		<ul class="divide-line divide-y">
			{#each data.downloads as d (d.id)}
				{@const name = d.name ?? "A title no longer here"}
				<li class="flex flex-wrap items-center gap-x-4 gap-y-2 py-3">
					<div class="grid min-w-0 flex-1 gap-1">
						{#if d.name}
							<a
								href="/titles/{d.title_id}"
								class="text-ink truncate font-semibold hover:underline"
							>
								{name}
							</a>
						{:else}
							<p class="text-ink-3 truncate font-semibold">{name}</p>
						{/if}
						<p class="text-ink-3 text-sm">
							{[
								d.method === "direct"
									? "Original"
									: [
											d.max_width && `${d.max_width} wide`,
											d.max_bitrate_kbps &&
												`up to ${bitrate(d.max_bitrate_kbps)}`,
										]
											.filter(Boolean)
											.join(", "),
								states[d.state],
								d.size_bytes && bytes(d.size_bytes),
							]
								.filter(Boolean)
								.join(" · ")}
						</p>
						{#if d.state === "converting"}
							<Progress
								value={d.progress * 100}
								aria-label="Converting {name}"
								class="max-w-sm"
							/>
						{/if}
						{#if d.error}
							<p class="text-destructive text-sm">{d.error}</p>
						{/if}
					</div>
					{#if d.state === "ready" && d.url}
						<Button href={d.url} download variant="outline" size="sm">
							<DownloadIcon />Save
							<span class="sr-only">{name}</span>
						</Button>
					{/if}
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label="Remove {name}"
						onclick={() => remove(d.id)}
					>
						<XIcon />
					</Button>
				</li>
			{/each}
		</ul>
	{:else}
		<p class="text-ink-3">
			Nothing downloaded. Choose Download from a film's or an episode's menu.
		</p>
	{/if}
</div>
