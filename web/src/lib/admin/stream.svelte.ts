import { createContext } from "svelte";
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
	"webhook.test",
] as const satisfies readonly ("snapshot" | EventKind)[];

// A refused or dropped stream is not retried by the browser once it has
// closed, so it is opened again after this long.
const retryAfter = 5_000;

// The admin event stream, as the dashboard's pages read it.
export class LiveStream {
	state = $state.raw<Live>(idle);
	connected = $state(false);

	// Opens the stream and keeps it open until the returned function is called.
	// `heard` is told each event's name after it has been applied.
	open(heard: (name: string) => void): () => void {
		let source: EventSource | undefined;
		let timer: ReturnType<typeof setTimeout> | undefined;
		const connect = () => {
			source = new EventSource("/api/v1/admin/events");
			for (const name of names) {
				source.addEventListener(name, (message) => {
					this.state = apply(this.state, name, JSON.parse(message.data));
					this.connected = true;
					heard(name);
				});
			}
			source.onerror = () => {
				this.connected = false;
				if (source?.readyState === EventSource.CLOSED) {
					timer = setTimeout(connect, retryAfter);
				}
			};
		};
		connect();
		return () => {
			clearTimeout(timer);
			source?.close();
		};
	}
}

export const [liveStream, setLiveStream] = createContext<LiveStream>();
