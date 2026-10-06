<script lang="ts">
import { onMount } from "svelte";
import { invalidate } from "$app/navigation";
import { LiveStream, setLiveStream } from "#lib/admin/stream.svelte.js";

let { children } = $props();

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

{@render children()}
