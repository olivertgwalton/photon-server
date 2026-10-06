<script lang="ts">
import SearchIcon from "@lucide/svelte/icons/search";
import { page } from "$app/state";
import ActivityMenu from "#lib/components/ActivityMenu.svelte";
import AppSidebar from "#lib/components/AppSidebar.svelte";
import PlaylistPicker from "#lib/components/PlaylistPicker.svelte";
import ProfileMenu from "#lib/components/ProfileMenu.svelte";
import * as Sidebar from "#lib/components/ui/sidebar/index.js";
import { onMount } from "svelte";
import { invalidate } from "$app/navigation";
import { LiveStream, setLiveStream } from "#lib/admin/stream.svelte.js";
import { live } from "#lib/live.svelte.js";

let { data, children } = $props();

// The server's changes, for as long as the shell is open.
$effect(() => live.connect());

// What a page loaded that an event says has changed. A busy scan tells many
// jobs a second, so each is reloaded once a second at most.
const changes: Record<string, string> = {
	"task.started": "admin:tasks",
	"task.finished": "admin:tasks",
	"task.failed": "admin:tasks",
	"job.started": "admin:jobs",
	"job.finished": "admin:jobs",
	"job.failed": "admin:jobs",
	"job.dead": "admin:jobs",
	"playback.started": "admin:playbacks",
	"playback.stopped": "admin:playbacks",
	"library.added": "admin:libraries",
	"library.removed": "admin:libraries",
	"profile.added": "admin:profiles",
	"profile.removed": "admin:profiles",
};

// What the server is doing, for an admin: the activity menu and the dashboard
// read the one stream.
const stream = setLiveStream(new LiveStream());

onMount(() => {
	if (data.me.role !== "admin") return;
	const pending = new Set<string>();
	let timer: ReturnType<typeof setTimeout> | undefined;
	const close = stream.open((name) => {
		const changed = changes[name];
		if (!changed) return;
		pending.add(changed);
		timer ??= setTimeout(() => {
			for (const key of pending) invalidate(key);
			pending.clear();
			timer = undefined;
		}, 1_000);
	});
	return () => {
		clearTimeout(timer);
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
	<AppSidebar libraries={data.libraries} />
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
		<main id="main" tabindex="-1" class="min-w-0 flex-1 px-3 py-6 sm:px-6">
			{@render children()}
		</main>
	</Sidebar.Inset>
</Sidebar.Provider>

<PlaylistPicker />
