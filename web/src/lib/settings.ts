// The settings search finds inside a page, beside the page's own name: each by
// the id of what sets it, so a result opens the page there, with the other
// words someone might look for it by.
export const settings: Record<string, [string, string, string][]> = {
	"/settings": [
		["name", "Name", "rename"],
		["current", "Password", "change"],
		["pin", "PIN", "lock code switch"],
	],
	"/settings/playback": [
		["quality", "Quality", "resolution bitrate transcode"],
		["audio-language", "Audio language", "sound dub"],
		["audio-default", "Play the default track", "audio sound"],
		["subtitle-mode", "Subtitles", "captions cc forced"],
		["subtitle-language", "Subtitle language", "captions cc"],
		["remember-audio", "Remember the sound chosen", "audio track"],
		["remember-subtitles", "Remember the subtitles chosen", "captions"],
		["autoplay", "Play the next episode", "autoplay binge"],
		["skip-intro", "Intros and recaps", "skip"],
		["skip-credits", "Credits and previews", "skip outro"],
		["theme-music", "Play theme music", "tune song"],
	],
	"/settings/server/general": [
		["server-name", "Server name", "rename"],
		["server-language", "Metadata language", "locale"],
		["server-country", "Certification country", "age rating"],
	],
	"/settings/server/network": [
		["secure-connections", "Secure connections", "https tls"],
		["certificate", "Certificate", "https tls pem"],
		["key", "Certificate key", "https tls private pem"],
		["public-url", "Public address", "url domain"],
		["trusted-proxies", "Trusted proxies", "reverse proxy forwarded"],
		["discovery", "Discovery", "find local network"],
		["jellyfin", "Jellyfin apps", "clients"],
		["jellyfin-port", "Jellyfin port", "8096"],
		["local-networks", "Local networks", "home lan tailnet"],
		["remote-max", "Remote stream limit", "bitrate mbps upload internet"],
	],
	"/settings/server/storage": [
		["kind", "Where files are kept", "disk bucket s3 artwork avatars"],
	],
	"/settings/server/tasks": [
		["window-start", "Maintenance window", "hours night schedule"],
		["window-zone", "Maintenance time zone", "timezone"],
		["window-previews", "Make previews", "thumbnails seek"],
		["window-markers", "Find intros and credits", "markers skip"],
	],
};
