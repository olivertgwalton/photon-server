package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// openAPIType is the registered media type of an OpenAPI description in JSON.
const openAPIType = "application/vnd.oai.openapi+json"

// param is a path or query parameter: of is a value of its type, a list the parameter repeated.
type param struct {
	name string
	of   any
	doc  string
}

// asFile is a body or reply that is a file, by the types it may be.
type asFile []string

// asStream is a reply of Server-Sent Events, by each event's name and the JSON its data carries.
type asStream map[string]any

var (
	limitParam = param{"limit", 0, "How many to answer, from 1 to " + strconv.Itoa(maxWallLimit) + "."}
	pageParams = []param{{"offset", 0, "Where the page starts, from 0."}, limitParam}
	// signatureParams are a signed address's: GET it as the server gave it.
	signatureParams = []param{
		{"exp", "", "When the address lapses, as the server signed it."},
		{"sig", "", "The server's signature of the path and exp."},
	}
)

var wildcard = regexp.MustCompile(`\{(\w+)\}`)

// Describe answers the API's OpenAPI description for a server of this version, as
// GET /api/v1/openapi.json does, with no server behind it.
func Describe(info domain.Info) ([]byte, error) {
	return describe(info, new(API).routes())
}

// describe answers the API's OpenAPI description, from its routes.
func describe(info domain.Info, routes []route) ([]byte, error) {
	s := newSchemas()
	problemRef := s.of(reflect.TypeFor[problem]())
	paths := map[string]map[string]any{}
	for _, r := range routes {
		method, path, _ := strings.Cut(r.pattern, " ")
		op, err := operation(s, r, path)
		if err != nil {
			return nil, err
		}
		if paths[path] == nil {
			paths[path] = map[string]any{}
		}
		paths[path][strings.ToLower(method)] = op
	}
	if s.err != nil {
		return nil, s.err
	}
	return json.Marshal(map[string]any{
		"openapi": "3.1.1",
		"info": map[string]any{
			"title":   "photon-server",
			"version": info.Version,
			"license": map[string]any{"name": "Apache-2.0", "identifier": "Apache-2.0"},
			"description": "A media server's API. Every refusal is a problem (RFC 9457) whose code says which. " +
				"A route marked admin answers only an admin's session. A signed address, as play answers, " +
				"is fetched as given, with no token: the signature is in its exp and sig, or in its path.",
		},
		"paths": paths,
		"components": map[string]any{
			"schemas": s.defs,
			"responses": map[string]any{
				"Problem": map[string]any{
					"description": "A refusal.",
					"content":     map[string]any{"application/problem+json": map[string]any{"schema": problemRef}},
				},
			},
			"securitySchemes": map[string]any{
				"session": map[string]any{
					"type": "http", "scheme": "bearer",
					"description": "A device's token, from signing in or pairing, in the Authorization header.",
				},
				"cookie": map[string]any{
					"type": "apiKey", "in": "cookie", "name": sessionCookie,
					"description": "The web app's session: the same token, kept by the browser. " +
						"A write carrying it is refused as forbidden unless it comes from this server's own pages.",
				},
			},
		},
	})
}

func operation(s *schemas, r route, path string) (map[string]any, error) {
	if r.summary == "" || r.status == 0 {
		return nil, fmt.Errorf("%s has no summary or status", r.pattern)
	}
	if (r.reply == nil) != (r.status == http.StatusNoContent || r.status == http.StatusAccepted) {
		return nil, fmt.Errorf("%s answers %d with reply %T", r.pattern, r.status, r.reply)
	}
	var params []any
	for _, m := range wildcard.FindAllStringSubmatch(path, -1) {
		p := param{m[1], uuid.UUID{}, ""}
		if i := slices.IndexFunc(r.path, func(p param) bool { return p.name == m[1] }); i >= 0 {
			p = r.path[i]
		}
		params = append(params, parameter(s, p, "path"))
	}
	for _, p := range r.path {
		if !strings.Contains(path, "{"+p.name+"}") {
			return nil, fmt.Errorf("%s has no wildcard %s", r.pattern, p.name)
		}
	}
	for _, p := range r.query {
		params = append(params, parameter(s, p, "query"))
	}
	op := map[string]any{
		"summary":  r.summary,
		"x-access": string(r.access),
		"security": []any{},
	}
	if r.access == signedIn || r.access == admin {
		op["security"] = []any{map[string]any{"session": []string{}}, map[string]any{"cookie": []string{}}}
	}
	if r.access == admin {
		op["description"] = "Admin only."
	}
	if params != nil {
		op["parameters"] = params
	}
	switch body := r.body.(type) {
	case nil:
	case asFile:
		content := map[string]any{}
		for _, t := range body {
			content[t] = map[string]any{"schema": map[string]any{"type": "string", "contentMediaType": t}}
		}
		op["requestBody"] = map[string]any{"required": true, "content": content}
	default:
		op["requestBody"] = map[string]any{
			"required": true,
			"content":  map[string]any{"application/json": map[string]any{"schema": s.of(reflect.TypeOf(r.body))}},
		}
	}
	success := map[string]any{"description": http.StatusText(r.status)}
	switch reply := r.reply.(type) {
	case nil:
	case asFile:
		content := map[string]any{}
		for _, t := range reply {
			content[t] = map[string]any{}
		}
		success["content"] = content
	case asStream:
		events := map[string]any{}
		for name, data := range reply {
			events[name] = s.of(reflect.TypeOf(data))
		}
		success["content"] = map[string]any{"text/event-stream": map[string]any{
			"schema": map[string]any{"type": "string"}, "x-events": events,
		}}
	default:
		success["content"] = map[string]any{"application/json": map[string]any{"schema": s.of(reflect.TypeOf(reply))}}
	}
	responses := map[string]any{
		strconv.Itoa(r.status): success,
		"default":              map[string]any{"$ref": "#/components/responses/Problem"},
	}
	for status, refusal := range r.refusals {
		responses[strconv.Itoa(status)] = map[string]any{
			"description": http.StatusText(status),
			"content":     map[string]any{"application/problem+json": map[string]any{"schema": s.of(reflect.TypeOf(refusal))}},
		}
	}
	op["responses"] = responses
	return op, nil
}

func parameter(s *schemas, p param, in string) map[string]any {
	out := map[string]any{"name": p.name, "in": in, "schema": s.of(reflect.TypeOf(p.of))}
	if in == "path" {
		out["required"] = true
	}
	if p.doc != "" {
		out["description"] = p.doc
	}
	return out
}

func (a *API) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", openAPIType)
	_, _ = w.Write(a.description)
}
