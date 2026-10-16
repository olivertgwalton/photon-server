---
title: "API reference"
summary: "Every route photon-server answers, generated from its OpenAPI description."
---

The reference is generated from the server's own OpenAPI description, so it is never behind the
code. A running server serves it at `/api/v1/openapi.json`, and its web app draws it at
`/api-docs`. Without a server, the binary prints it:

```sh
photon-server openapi > openapi.json
```
