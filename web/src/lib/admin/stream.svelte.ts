import { createContext } from "svelte";
import { openStream } from "#lib/live.svelte.js";
import { apply, type EventKind, idle, type Live } from "./live.js";

// Every name the stream sends an event by: an EventSource hears only the names
// it listens for.
const names = [
	"snapshot",
	"playback.started",
	"playback.paused",
	"playback.resumed",
	"playback.stopped",
	"auth.signed_in",
	"auth.sign_in_refused",
	"profile.added",
	"profile.removed",
	"library.added",
	"library.removed",
	"library.scanned",
	"library.titles_added",
	"scan.progress",
	"task.started",
	"task.finished",
	"task.failed",
	"backup.made",
	"job.started",
	"job.finished",
	"job.failed",
	"job.dead",
	"jobs.progress",
	"webhook.test",
	"maintenance.changed",
] as const satisfies readonly ("snapshot" | EventKind)[];

// The admin event stream, as the dashboard's pages read it.
export class LiveStream {
	state = $state.raw<Live>(idle);
	connected = $state(false);

	// Opens the stream and keeps it open until the returned function is called.
	// `heard` is told each event's name after it has been applied.
	open(heard: (name: string) => void): () => void {
		return openStream(
			"/api/v1/admin/events",
			Object.fromEntries(
				names.map((name) => [
					name,
					(message: MessageEvent<string>) => {
						this.state = apply(this.state, name, JSON.parse(message.data));
						this.connected = true;
						heard(name);
					},
				]),
			),
			() => {
				this.connected = false;
			},
		);
	}
}

export const [liveStream, setLiveStream] = createContext<LiveStream>();
