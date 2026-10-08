// A month of the calendar, and the whole weeks, Monday first, its grid shows.
// Days are written as the server writes them, 2006-01-02, and counted in UTC
// so a change of the clocks never skips or repeats one.

export type Month = { year: number; month: number };

const dayMS = 24 * 60 * 60 * 1000;

function dayKey(d: Date): string {
	return d.toISOString().slice(0, 10);
}

// The day it is where the reader is.
export function today(now: Date): string {
	return `${monthKey({ year: now.getFullYear(), month: now.getMonth() + 1 })}-${String(now.getDate()).padStart(2, "0")}`;
}

// The month a page names as 2006-01, else the one today is in.
export function monthOf(named: string | null, now: Date): Month {
	const m = named?.match(/^(\d{4})-(\d{2})$/);
	if (m && Number(m[2]) >= 1 && Number(m[2]) <= 12)
		return { year: Number(m[1]), month: Number(m[2]) };
	return { year: now.getFullYear(), month: now.getMonth() + 1 };
}

export function monthKey({ year, month }: Month): string {
	return `${year}-${String(month).padStart(2, "0")}`;
}

export function shift({ year, month }: Month, by: number): Month {
	const d = new Date(Date.UTC(year, month - 1 + by, 1));
	return { year: d.getUTCFullYear(), month: d.getUTCMonth() + 1 };
}

// The days the upcoming list asks for, as Jellyfin's: from yesterday, six
// weeks, the most the server answers at once.
export function upcoming(today: string): string[] {
	const start = Date.parse(today) - dayMS;
	return Array.from({ length: 42 }, (_, n) =>
		dayKey(new Date(start + n * dayMS)),
	);
}

// What the upcoming list heads a day with: yesterday, today and tomorrow by
// name, the rest by date.
export function dayLabel(day: string, today: string, locale?: string): string {
	const away = (Date.parse(day) - Date.parse(today)) / dayMS;
	const near = { [-1]: "Yesterday", 0: "Today", 1: "Tomorrow" }[away];
	return (
		near ??
		new Date(Date.parse(day)).toLocaleDateString(locale, {
			timeZone: "UTC",
			weekday: "long",
			day: "numeric",
			month: "long",
		})
	);
}

// Every day from the Monday the month's first week starts on to the Sunday its
// last ends on: four to six weeks.
export function weeks({ year, month }: Month): string[] {
	const first = Date.UTC(year, month - 1, 1);
	const last = Date.UTC(year, month, 0);
	const start = first - ((new Date(first).getUTCDay() + 6) % 7) * dayMS;
	const end = last + ((7 - new Date(last).getUTCDay()) % 7) * dayMS;
	const out: string[] = [];
	for (let t = start; t <= end; t += dayMS) out.push(dayKey(new Date(t)));
	return out;
}
