// Whether the server is restoring its database: every node stops for it,
// and the app waits for one to answer again.
export const restoring = $state({ on: false });
