// A submitted form's fields, its default prevented.
export function fields(event: SubmitEvent): FormData {
	event.preventDefault();
	return new FormData(event.currentTarget as HTMLFormElement);
}

// A text field without the spaces around it.
export function text(form: FormData, name: string): string {
	return textAsTyped(form, name).trim();
}

export function textAsTyped(form: FormData, name: string): string {
	return String(form.get(name) ?? "");
}
