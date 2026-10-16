// Google's Cast sender, which plays a title on a Chromecast through Google's
// Default Media Receiver. The receiver fetches the stream from the server
// itself, at the signed address a play answers.
const sender =
	"https://www.gstatic.com/cv/js/sender/v1/cast_sender.js?loadCastFramework=1";

let loading: Promise<cast.framework.CastContext | undefined> | undefined;
let loaded: cast.framework.CastContext | undefined;

// The Cast context, once the sender has loaded, where the browser casts:
// Chrome and the browsers built on it, which say so in userAgentData, and
// only in a secure context, as the sender refuses any other. A page at the
// browser's own loopback is secure but gives the Chromecast an address it
// cannot reach. In any other case Google's script is not loaded.
export function castContext(): Promise<cast.framework.CastContext | undefined> {
	if (
		!isSecureContext ||
		!("userAgentData" in navigator) ||
		!reachable(location.origin)
	)
		return Promise.resolve(undefined);
	loading ??= new Promise((resolve) => {
		window.__onGCastApiAvailable = (available) => {
			if (!available) return resolve(undefined);
			loaded = cast.framework.CastContext.getInstance();
			loaded.setOptions({
				receiverApplicationId: chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
				autoJoinPolicy: chrome.cast.AutoJoinPolicy.ORIGIN_SCOPED,
			});
			resolve(loaded);
		};
		const script = document.createElement("script");
		script.src = sender;
		script.onerror = () => resolve(undefined);
		document.head.append(script);
	});
	return loading;
}

// The session playing on a Chromecast now, where the sender has loaded.
export function castSession(): cast.framework.CastSession | undefined {
	return loaded?.getCurrentSession() ?? undefined;
}

// Whether a Chromecast on the network can fetch from a page's origin: not
// from one the browser reaches on its own loopback.
export function reachable(origin: string): boolean {
	const host = new URL(origin).hostname;
	return !(
		host === "localhost" ||
		host.endsWith(".localhost") ||
		host === "[::1]" ||
		host.startsWith("127.")
	);
}

// What the receiver is asked to play: a playback's HLS playlist, its parts
// fMP4, from at seconds, with the title and picture it shows.
export function castRequest(
	playlist: string,
	at: number,
	heading: { title: string; subtitle?: string; picture?: string },
): chrome.cast.media.LoadRequest {
	const info = new chrome.cast.media.MediaInfo(
		new URL(playlist, location.href).href,
		"application/vnd.apple.mpegurl",
	);
	info.streamType = chrome.cast.media.StreamType.BUFFERED;
	// The sender's types have neither enum.
	const formats = chrome.cast.media as unknown as {
		HlsSegmentFormat: { FMP4: string };
		HlsVideoSegmentFormat: { FMP4: string };
	};
	Object.assign(info, {
		hlsSegmentFormat: formats.HlsSegmentFormat.FMP4,
		hlsVideoSegmentFormat: formats.HlsVideoSegmentFormat.FMP4,
	});
	const metadata = new chrome.cast.media.GenericMediaMetadata();
	metadata.title = heading.title;
	if (heading.subtitle) metadata.subtitle = heading.subtitle;
	if (heading.picture)
		metadata.images = [
			new chrome.cast.Image(new URL(heading.picture, location.href).href),
		];
	info.metadata = metadata;
	const request = new chrome.cast.media.LoadRequest(info);
	request.currentTime = at;
	request.autoplay = true;
	return request;
}
