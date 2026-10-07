<script lang="ts">
import DatabaseBackupIcon from "@lucide/svelte/icons/database-backup";
import EllipsisVerticalIcon from "@lucide/svelte/icons/ellipsis-vertical";
import ArrowUpDownIcon from "@lucide/svelte/icons/arrow-up-down";
import FolderSyncIcon from "@lucide/svelte/icons/folder-sync";
import PencilIcon from "@lucide/svelte/icons/pencil";
import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
import Trash2Icon from "@lucide/svelte/icons/trash-2";
import { goto } from "$app/navigation";
import { page } from "$app/state";
import {
	refreshLibrary,
	removeLibrary,
	scanLibrary,
} from "#lib/actions.svelte.js";
import type { components } from "#lib/api/schema.js";
import * as DropdownMenu from "#lib/components/ui/dropdown-menu/index.js";

type Library = components["schemas"]["Library"];

// What can be done to a library from beside its name, as Plex's sidebar
// offers: anyone puts the libraries in their own order, and an admin reaches
// its settings and scans.
let {
	library,
	onreorder,
}: { library: Pick<Library, "id" | "name">; onreorder: () => void } = $props();

const admin = $derived(page.data.me?.role === "admin");
</script>

<DropdownMenu.Root>
	<!-- Shown on hover where there is one; always where a finger has none. -->
	<DropdownMenu.Trigger
		class="text-ink-2 hover:bg-raise hover:text-ink focus-visible:outline-signal data-[state=open]:bg-raise absolute top-1.5 right-1 grid size-6 place-items-center rounded-md transition-opacity group-data-[collapsible=icon]:hidden md:opacity-0 md:group-hover/library:opacity-100 md:focus-visible:opacity-100 md:data-[state=open]:opacity-100"
		aria-label="More for {library.name}"
	>
		<EllipsisVerticalIcon class="size-4" />
	</DropdownMenu.Trigger>
	<DropdownMenu.Content side="right" align="start" class="w-60">
		<DropdownMenu.Item onSelect={onreorder}>
			<ArrowUpDownIcon />Reorder
		</DropdownMenu.Item>
		{#if admin}
			<DropdownMenu.Separator />
			<DropdownMenu.Item
				onSelect={() => goto(`/settings/server/libraries/${library.id}`)}
			>
				<PencilIcon />Edit…
			</DropdownMenu.Item>
			<DropdownMenu.Item onSelect={() => scanLibrary(library.id, library.name)}>
				<FolderSyncIcon />Scan library files
			</DropdownMenu.Item>
			<DropdownMenu.Item
				onSelect={() => refreshLibrary(library.id, library.name, "missing")}
			>
				<RefreshCwIcon />Refresh missing metadata
			</DropdownMenu.Item>
			<DropdownMenu.Item
				onSelect={() => refreshLibrary(library.id, library.name, "all")}
			>
				<DatabaseBackupIcon />Refresh all metadata
			</DropdownMenu.Item>
			<DropdownMenu.Separator />
			<DropdownMenu.Item
				variant="destructive"
				onSelect={() => removeLibrary(library.id, library.name)}
			>
				<Trash2Icon />Remove…
			</DropdownMenu.Item>
		{/if}
	</DropdownMenu.Content>
</DropdownMenu.Root>
