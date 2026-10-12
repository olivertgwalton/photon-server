declare global {
	namespace App {
		interface PageState {
			// Where the title playing is in the queue it was opened from.
			queued?: number;
		}
	}
}

export {};
