// What the devices list calls this browser: the browser and the system it
// runs on, as Plex's and Jellyfin's lists do. Edge and Opera say Chrome too,
// and Chrome says Safari, so the order matters.
const browsers: [RegExp, string][] = [
	[/Edg\//, "Edge"],
	[/OPR\//, "Opera"],
	[/Firefox\//, "Firefox"],
	[/Chrome\//, "Chrome"],
	[/Safari\//, "Safari"],
];

const systems: [RegExp, string][] = [
	[/iPhone|iPad/, "iOS"],
	[/Android/, "Android"],
	[/Mac OS X/, "macOS"],
	[/Windows/, "Windows"],
	[/CrOS/, "ChromeOS"],
	[/Linux/, "Linux"],
];

export function deviceName(userAgent: string | null): string {
	const ua = userAgent ?? "";
	const browser = browsers.find(([re]) => re.test(ua))?.[1] ?? "Browser";
	const system = systems.find(([re]) => re.test(ua))?.[1];
	return system ? `${browser} on ${system}` : browser;
}

// The client the server records every web sign-in under.
export const CLIENT = "Photon Web";
