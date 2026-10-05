import createClient from "openapi-fetch";
import type { paths } from "./schema.js";

// The browser's API: the same origin, through the proxy, which adds the
// session's token. Call it from the browser only; server code uses
// `event.locals.api`.
export const api = createClient<paths>();
