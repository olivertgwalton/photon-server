<script lang="ts">
import FilmIcon from "@lucide/svelte/icons/film";
import TvIcon from "@lucide/svelte/icons/tv";
import { act } from "#lib/admin/act.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { client } from "#lib/api/client.js";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import ScanProgress from "#lib/components/admin/ScanProgress.svelte";
import { Button } from "#lib/components/ui/button/index.js";

let { data } = $props();

const live = liveStream();
const api = client();
const path = (id: string) => ({ params: { path: { id } } });
</script>

<svelte:head><title>Libraries · Dashboard · Photon</title></svelte:head>

<div class="flex flex-wrap items-center justify-between gap-4">
	<h1 class="title">Libraries</h1>
	<Button href="/admin/libraries/new">Add a library</Button>
</div>

{#if data.libraries.length}
	<ul class="grid gap-3">
		{#each data.libraries as library (library.id)}
			{@const scan = live.state.scans.find((s) => s.library_id === library.id)}
			<li
				class="bg-raise grid gap-3 rounded-xl p-4 sm:grid-cols-[1fr_auto] sm:items-center"
			>
				<div class="flex min-w-0 items-start gap-3">
					{#if library.kind === "movies"}
						<FilmIcon
							class="text-ink-3 mt-0.5 size-5 shrink-0"
							aria-label="Films"
						/>
					{:else}
						<TvIcon
							class="text-ink-3 mt-0.5 size-5 shrink-0"
							aria-label="Shows"
						/>
					{/if}
					<div class="grid min-w-0 flex-1 gap-1">
						<h2 class="heading">
							<a href="/admin/libraries/{library.id}" class="hover:underline">
								{library.name}
							</a>
						</h2>
						<p class="text-ink-3 truncate font-mono text-xs">{library.root}</p>
						{#if scan}
							<ScanProgress {scan} name={library.name} />
						{/if}
					</div>
				</div>
				<div class="flex flex-wrap gap-2">
					<Button
						variant="outline"
						size="sm"
						disabled={!!scan}
						onclick={() =>
							act(
								api.POST("/api/v1/admin/libraries/{id}/scan", path(library.id)),
								`${library.name} is being scanned.`,
							)}
					>
						{scan ? "Scanning…" : "Scan now"}
						<span class="sr-only">{library.name}</span>
					</Button>
					<Button
						href="/admin/libraries/{library.id}"
						variant="outline"
						size="sm"
					>
						Edit <span class="sr-only">{library.name}</span>
					</Button>
					<ConfirmButton
						label="Remove"
						hidden={library.name}
						title="Remove {library.name}?"
						confirm="Remove library"
						onconfirm={() =>
							act(
								api.DELETE("/api/v1/admin/libraries/{id}", path(library.id)),
								`${library.name} was removed.`,
							)}
					>
						Its titles, and what everyone has watched of them, are forgotten.
						The files on disk are not touched.
					</ConfirmButton>
				</div>
			</li>
		{/each}
	</ul>
{:else}
	<p class="text-ink-3 text-sm">
		No libraries yet. Add one to start scanning a folder of films or shows.
	</p>
{/if}
