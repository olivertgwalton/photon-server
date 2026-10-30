import { createContext } from "svelte";
import { apply, idle, type Live } from "./live.ts";

// What the server is doing, as its feed on the events socket tells it to the
// dashboard's pages and the activity menu.
export class LiveStream {
	state = $state.raw<Live>(idle);
	connected = $state(false);

	hear(name: string, data: unknown) {
		this.state = apply(this.state, name, data);
		this.connected = true;
	}

	drop() {
		this.connected = false;
	}
}

export const [liveStream, setLiveStream] = createContext<LiveStream>();
