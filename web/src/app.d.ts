import type { Client } from "openapi-fetch";
import type { components, paths } from "#lib/api/schema.js";

declare global {
	namespace App {
		interface Locals {
			// The API as this request's session, signed in or not.
			api: Client<paths>;
			// The session's token and profile, absent when signed out.
			session?: {
				token: string;
				profile: components["schemas"]["Profile"];
			};
		}
	}
}
