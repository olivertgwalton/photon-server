import { castSession } from "./cast.ts";

// What a player is told of the Chromecast: its session started, joined or
// ended, where the receiver has got to, and that it plays or is paused.
type Listener = {
	moved: (seconds: number) => void;
	playing: () => void;
	paused: () => void;
	changed: () => void;
};

// Whether a Chromecast is on the network, through Google's sender, and the
// session playing on one.
export class Casting {
	devices = $state(false);
	session = $state.raw<cast.framework.CastSession>();
	paused = $state(true);
	waiting = $state(false);

	#remote:
		| {
				player: cast.framework.RemotePlayer;
				controller: cast.framework.RemotePlayerController;
		  }
		| undefined;

	// The session playing now, joined where the sender found one since.
	join(): cast.framework.CastSession | undefined {
		this.session ??= castSession();
		return this.session;
	}

	get device(): string | undefined {
		return this.session?.getCastDevice().friendlyName;
	}

	// Follows the Chromecasts on the network and the session on one: a session
	// started, or one already playing from another page of this server, is
	// joined, and one ended is left.
	watch(context: cast.framework.CastContext, on: Listener): () => void {
		const {
			CastContextEventType,
			CastState,
			RemotePlayerEventType,
			SessionState,
		} = cast.framework;
		const player = new cast.framework.RemotePlayer();
		const controller = new cast.framework.RemotePlayerController(player);
		this.#remote = { player, controller };
		this.devices = context.getCastState() !== CastState.NO_DEVICES_AVAILABLE;
		const devices = (e: cast.framework.CastStateEventData) => {
			this.devices = e.castState !== CastState.NO_DEVICES_AVAILABLE;
		};
		const sessions = (e: cast.framework.SessionStateEventData) => {
			if (
				e.sessionState === SessionState.SESSION_STARTED ||
				e.sessionState === SessionState.SESSION_RESUMED
			) {
				if (this.session === e.session) return;
				this.session = e.session;
				on.changed();
			} else if (
				e.sessionState === SessionState.SESSION_ENDED &&
				this.session
			) {
				this.session = undefined;
				on.changed();
			}
		};
		const moved = () => {
			if (this.session && player.isMediaLoaded) on.moved(player.currentTime);
		};
		const state = () => {
			if (!this.session) return;
			const { PlayerState } = chrome.cast.media;
			this.waiting = player.playerState === PlayerState.BUFFERING;
			if (player.playerState === PlayerState.PLAYING) {
				this.paused = false;
				on.playing();
			} else if (player.playerState === PlayerState.PAUSED) {
				this.paused = true;
				on.paused();
			}
		};
		context.addEventListener(CastContextEventType.CAST_STATE_CHANGED, devices);
		context.addEventListener(
			CastContextEventType.SESSION_STATE_CHANGED,
			sessions,
		);
		controller.addEventListener(
			RemotePlayerEventType.CURRENT_TIME_CHANGED,
			moved,
		);
		controller.addEventListener(
			RemotePlayerEventType.PLAYER_STATE_CHANGED,
			state,
		);
		const current = context.getCurrentSession();
		if (current && this.session !== current) {
			this.session = current;
			on.changed();
		}
		return () => {
			context.removeEventListener(
				CastContextEventType.CAST_STATE_CHANGED,
				devices,
			);
			context.removeEventListener(
				CastContextEventType.SESSION_STATE_CHANGED,
				sessions,
			);
			controller.removeEventListener(
				RemotePlayerEventType.CURRENT_TIME_CHANGED,
				moved,
			);
			controller.removeEventListener(
				RemotePlayerEventType.PLAYER_STATE_CHANGED,
				state,
			);
			this.#remote = undefined;
		};
	}

	// Plays `request` on the session, telling `finished` when the receiver plays
	// it to its end. Answers whether the receiver took it.
	async play(
		session: cast.framework.CastSession,
		request: chrome.cast.media.LoadRequest,
		finished: () => void,
	): Promise<boolean> {
		this.waiting = true;
		const failed = await session
			.loadMedia(request)
			.catch((code) => code ?? "failed");
		if (failed) return false;
		const media = session.getMediaSession();
		media?.addUpdateListener(() => {
			if (
				media.playerState === chrome.cast.media.PlayerState.IDLE &&
				media.idleReason === chrome.cast.media.IdleReason.FINISHED
			)
				finished();
		});
		return true;
	}

	// Shows the HLS playlist's rendition n, which the receiver lists among its
	// text tracks in the playlist's order; none for any other n.
	subtitle(n: number) {
		const media = this.session?.getMediaSession();
		if (!media) return;
		const text = (media.media?.tracks ?? []).filter(
			(t) => t.type === chrome.cast.media.TrackType.TEXT,
		);
		const shown = text[n];
		media.editTracksInfo(
			new chrome.cast.media.EditTracksInfoRequest(shown ? [shown.trackId] : []),
			() => {},
			() => {},
		);
	}

	// Answers whether there was a receiver to seek.
	seek(seconds: number): boolean {
		if (!this.#remote) return false;
		this.#remote.player.currentTime = seconds;
		this.#remote.controller.seek();
		return true;
	}

	toggle() {
		this.#remote?.controller.playOrPause();
	}

	// The server lets the playback go, so the receiver stops it too; a title
	// opened next on the same session plays on it again.
	stop() {
		this.session?.getMediaSession()?.stop(
			new chrome.cast.media.StopRequest(),
			() => {},
			() => {},
		);
	}
}
