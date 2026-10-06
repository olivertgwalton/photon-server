<script lang="ts">
import { onMount } from "svelte";
import { invalidate } from "$app/navigation";
import { page } from "$app/state";
import { LiveStream, setLiveStream } from "#lib/admin/stream.svelte.js";

let { children } = $props();

const sections = [
	["/admin", "Overview"],
	["/admin/libraries", "Libraries"],
	["/admin/profiles", "Profiles"],
	["/admin/providers", "Providers"],
	["/admin/collections", "Collections"],
	["/admin/tasks", "Tasks"],
	["/admin/jobs", "Jobs"],
	["/admin/activity", "Activity"],
	["/admin/history", "History"],
	["/admin/webhooks", "Webhooks"],
] as const;

function current(href: string) {
	const path = page.url.pathname;
	return href === "/admin"
		? path === href
		: path === href || path.startsWith(`${href}/`);
}

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

const live = setLiveStream(new LiveStream());

onMount(() => {
	const pending = new Set<string>();
	let timer: ReturnType<typeof setTimeout> | undefined;
	const close = live.open((name) => {
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

<div class="mx-auto grid max-w-7xl gap-6">
	<nav
		aria-label="Dashboard"
		class="-mx-3 overflow-x-auto px-3 sm:mx-0 sm:px-0"
	>
		<ul class="flex gap-1">
			{#each sections as [href, label] (href)}
				<li>
					<a
						{href}
						aria-current={current(href) ? "page" : undefined}
						class={[
							"block rounded-full px-3.5 py-1.5 text-sm font-semibold whitespace-nowrap transition-colors",
							current(href)
								? "bg-signal text-signal-ink"
								: "text-ink-2 hover:bg-raise hover:text-ink",
						]}
					>
						{label}
					</a>
				</li>
			{/each}
		</ul>
	</nav>
	{@render children()}
</div>
