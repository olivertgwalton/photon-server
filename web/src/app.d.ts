declare global {
	namespace App {
		interface PageState {
			// Where the title playing is in the queue it was opened from.
			queued?: number;
			// The episodes played in a row with nothing pressed, this one among them.
			run?: number;
		}
	}
}

export {};
