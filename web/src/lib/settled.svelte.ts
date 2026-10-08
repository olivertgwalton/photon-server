import { isHttpError, isRedirect } from "@sveltejs/kit";
import { goto } from "$app/navigation";

// The latest answer to what a load streams rather than awaits: a page draws
// what it has while the next answer is on its way. A refusal is the page's to say, and a redirect is followed, as `need`
// has them where a load is awaited.
export function settled<T>(promise: () => Promise<T>) {
	let value = $state<T>();
	let failed = $state<string>();
	let latest: Promise<T> | undefined;
	$effect(() => {
		const p = promise();
		latest = p;
		p.then(
			(v) => {
				if (latest !== p) return;
				value = v;
				failed = undefined;
			},
			(e: unknown) => {
				if (latest !== p) return;
				if (isRedirect(e)) goto(e.location);
				else failed = isHttpError(e) ? e.body.message : "Something went wrong.";
			},
		);
	});
	return {
		get value() {
			return value;
		},
		get failed() {
			return failed;
		},
	};
}
