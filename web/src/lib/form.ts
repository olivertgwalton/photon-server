// A submitted form's fields, its default prevented.
export function fields(event: SubmitEvent): FormData {
	event.preventDefault();
	return new FormData(event.currentTarget as HTMLFormElement);
}
