<script lang="ts">
import SearchIcon from "@lucide/svelte/icons/search";
import { LiveStream, setLiveStream } from "#lib/admin/stream.svelte.js";
import { adminAffected } from "#lib/changes.js";
import AppSidebar from "#lib/components/AppSidebar.svelte";
import ProfileMenu from "#lib/components/ProfileMenu.svelte";
import RestoringScreen from "#lib/components/RestoringScreen.svelte";
import TitleMenu from "#lib/components/TitleMenu.svelte";
import * as Sidebar from "#lib/components/ui/sidebar/index.js";
import { connectLive, invalidateLater } from "#lib/live.svelte.js";
import { Shell, setShell } from "#lib/shell.svelte.js";
import { page } from "$app/state";

let { data, children } = $props();
// The menu and dialogs drawn below, opened from any page in the shell.
const shell = setShell(new Shell());

// The server's changes, for as long as the shell is open.
$effect(() => connectLive());

// What the server is doing, for an admin: the activity menu and the dashboard
// read the one stream, and the pages it tells of changes reload.
const stream = setLiveStream(new LiveStream());
$effect(() => {
	if (data.me.role !== "admin") return;
	const later = invalidateLater();
	const close = stream.open((name) => later.add(adminAffected(name)));
	return () => {
		later.stop();
		close();
	};
});
</script>

<!-- Each lazily loaded component has a boundary of its own, so loading one
	holds up nothing else; one whose chunk fails to load (an open tab after a
	deploy) is logged and left out, and the shell stays. -->
{#snippet nothing()}
	<!-- Nothing, until it loads. -->
{/snippet}

<a
	href="#main"
	class="focus:bg-signal focus:text-signal-ink sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-100 focus:rounded-full focus:px-4 focus:py-2 focus:font-medium"
>
	Skip to content
</a>

<Sidebar.Provider open={data.sidebarOpen}>
	<AppSidebar
		libraries={data.libraries}
		collections={data.collections}
		pages={data.pages}
	/>
	<Sidebar.Inset>
		<header
			class="border-line bg-ground/85 sticky top-0 z-20 flex h-14 items-center gap-3 border-b px-3 backdrop-blur-xl sm:px-4"
		>
			<Sidebar.Trigger />
			<search class="mx-auto w-full max-w-md">
				<form action="/search" class="relative">
					<SearchIcon
						class="text-ink-3 pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2"
					/>
					<input
						type="search"
						name="q"
						aria-label="Search"
						placeholder="Search"
						value={page.url.pathname === "/search"
							? page.url.searchParams.get("q")
							: ""}
						class="bg-raise text-ink placeholder:text-ink-3 focus-visible:outline-signal h-9 w-full rounded-full pr-4 pl-9 text-sm"
					>
				</form>
			</search>
			{#if data.me.role === "admin"}
				<svelte:boundary pending={nothing} onerror={console.error}>
					{@const ActivityMenu = (
						await import("#lib/components/ActivityMenu.svelte")
					).default}
					<ActivityMenu libraries={data.libraries} />
				</svelte:boundary>
			{/if}
			<ProfileMenu profile={data.me} />
		</header>
		<main
			id="main"
			tabindex="-1"
			class="min-w-0 flex-1 px-3 py-6 [view-transition-name:page] sm:px-6"
		>
			{@render children()}
		</main>
	</Sidebar.Inset>
</Sidebar.Provider>

<RestoringScreen />
<TitleMenu />
<!-- Each dialog is asked for the first time a page opens it, and kept: most
	visits open none, and their first page should not wait on them. -->
{#if shell.picker.ids.length}
	<svelte:boundary pending={nothing} onerror={console.error}>
		{@const PlaylistPicker = (
			await import("#lib/components/PlaylistPicker.svelte")
		).default}
		<PlaylistPicker />
	</svelte:boundary>
{/if}
{#if shell.sharing.url}
	<svelte:boundary pending={nothing} onerror={console.error}>
		{@const ShareDialog = (await import("#lib/components/ShareDialog.svelte"))
			.default}
		<ShareDialog />
	</svelte:boundary>
{/if}
{#if shell.subtitleSearch.id}
	<svelte:boundary pending={nothing} onerror={console.error}>
		{@const SubtitleSearch = (
			await import("#lib/components/SubtitleSearch.svelte")
		).default}
		<SubtitleSearch />
	</svelte:boundary>
{/if}
{#if shell.versions.id}
	<svelte:boundary pending={nothing} onerror={console.error}>
		{@const VersionPicker = (
			await import("#lib/components/VersionPicker.svelte")
		).default}
		<VersionPicker />
	</svelte:boundary>
{/if}
{#if shell.confirming.title}
	<svelte:boundary pending={nothing} onerror={console.error}>
		{@const ConfirmDialog = (
			await import("#lib/components/ConfirmDialog.svelte")
		).default}
		<ConfirmDialog />
	</svelte:boundary>
{/if}
<!-- Asked for only by an admin's shell: the editor brings every admin form
	with it, which no one else's first page should wait on. -->
{#if data.me.role === "admin"}
	<svelte:boundary pending={nothing} onerror={console.error}>
		{@const TitleEditor = (
			await import("#lib/components/admin/TitleEditor.svelte")
		).default}
		<TitleEditor />
	</svelte:boundary>
	<svelte:boundary pending={nothing} onerror={console.error}>
		{@const RefreshDialog = (
			await import("#lib/components/admin/RefreshDialog.svelte")
		).default}
		<RefreshDialog />
	</svelte:boundary>
	<svelte:boundary pending={nothing} onerror={console.error}>
		{@const CollectionPicker = (
			await import("#lib/components/admin/CollectionPicker.svelte")
		).default}
		<CollectionPicker />
	</svelte:boundary>
{/if}
