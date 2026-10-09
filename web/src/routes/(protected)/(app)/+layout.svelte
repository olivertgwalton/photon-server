<script lang="ts">
import SearchIcon from "@lucide/svelte/icons/search";
import { page } from "$app/state";
import ActivityMenu from "#lib/components/ActivityMenu.svelte";
import AppSidebar from "#lib/components/AppSidebar.svelte";
import PlaylistPicker from "#lib/components/PlaylistPicker.svelte";
import ShareDialog from "#lib/components/ShareDialog.svelte";
import SubtitleSearch from "#lib/components/SubtitleSearch.svelte";
import VersionPicker from "#lib/components/VersionPicker.svelte";
import TitleEditor from "#lib/components/admin/TitleEditor.svelte";
import ConfirmDialog from "#lib/components/ConfirmDialog.svelte";
import ProfileMenu from "#lib/components/ProfileMenu.svelte";
import * as Sidebar from "#lib/components/ui/sidebar/index.js";
import { LiveStream, setLiveStream } from "#lib/admin/stream.svelte.js";
import { adminAffected } from "#lib/changes.js";
import { connectLive, invalidateLater } from "#lib/live.svelte.js";
import RestoringScreen from "#lib/components/RestoringScreen.svelte";

let { data, children } = $props();

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

<a
	href="#main"
	class="focus:bg-signal focus:text-signal-ink sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-100 focus:rounded-full focus:px-4 focus:py-2 focus:font-medium"
>
	Skip to content
</a>

<Sidebar.Provider open={data.sidebarOpen}>
	<AppSidebar libraries={data.libraries} pages={data.pages} />
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
				<ActivityMenu libraries={data.libraries} />
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

<PlaylistPicker />
<RestoringScreen />
<ShareDialog />
<SubtitleSearch />
<VersionPicker />
<ConfirmDialog />
{#if data.me.role === "admin"}
	<TitleEditor />
{/if}
