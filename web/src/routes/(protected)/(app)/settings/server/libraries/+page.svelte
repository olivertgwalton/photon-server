<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import DatabaseBackupIcon from "@lucide/svelte/icons/database-backup";
import FilmIcon from "@lucide/svelte/icons/film";
import LoaderCircleIcon from "@lucide/svelte/icons/loader-circle";
import PencilIcon from "@lucide/svelte/icons/pencil";
import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
import Trash2Icon from "@lucide/svelte/icons/trash-2";
import TvIcon from "@lucide/svelte/icons/tv";
import { act } from "#lib/act.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { client } from "#lib/api/client.js";
import { holding } from "#lib/format.js";
import ConfirmButton from "#lib/components/admin/ConfirmButton.svelte";
import IconButton from "#lib/components/IconButton.svelte";
import LibraryCheck from "#lib/components/admin/LibraryCheck.svelte";
import LibraryRefresh from "#lib/components/admin/LibraryRefresh.svelte";
import ScanProgress from "#lib/components/admin/ScanProgress.svelte";
import { Button } from "#lib/components/ui/button/index.js";

let { data } = $props();

const live = liveStream();
const api = client();
const path = (id: string) => ({ params: { path: { id } } });
</script>

<PageHeader
	title="Libraries"
	description="The folders the server reads films and shows from. A scan finds new and changed files; a refresh asks the metadata providers about the titles again."
>
	{#snippet actions()}
		<Button href="/settings/server/libraries/new">Add a library</Button>
	{/snippet}
</PageHeader>

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
							<a
								href="/settings/server/libraries/{library.id}"
								class="hover:underline"
							>
								{library.name}
							</a>
						</h2>
						<p class="text-ink-2 text-sm">
							{holding(library.kind, library.counts)}
						</p>
						<p class="text-ink-3 truncate font-mono text-xs">{library.root}</p>
						{#if scan}
							<ScanProgress {scan} name={library.name} />
						{/if}
					</div>
				</div>
				<div class="-ml-1.5 flex items-center gap-1 sm:ml-0">
					<IconButton
						label={scan ? "Scanning…" : "Scan now"}
						hidden={library.name}
						icon={scan ? LoaderCircleIcon : RefreshCwIcon}
						aria-busy={!!scan}
						disabled={!!scan}
						onclick={() =>
							act(
								api.POST("/api/v1/admin/libraries/{id}/scan", path(library.id)),
								`${library.name} is being scanned.`,
							)}
					/>
					<LibraryRefresh
						id={library.id}
						name={library.name}
						icon={DatabaseBackupIcon}
					/>
					<LibraryCheck id={library.id} name={library.name} />
					<IconButton
						label="Edit"
						hidden={library.name}
						icon={PencilIcon}
						href="/settings/server/libraries/{library.id}"
					/>
					<ConfirmButton
						label="Remove"
						hidden={library.name}
						icon={Trash2Icon}
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
