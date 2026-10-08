import { error } from "@sveltejs/kit";
import type { PageLoad } from "./$types";

// The whole of one craft of a person's work.
export const load: PageLoad = async ({ params, parent }) => {
	const { person, work } = await parent();
	const group = work.find((g) => g.slug === params.craft);
	if (!group) error(404, `${person.name} has no such work here.`);
	return { group };
};
