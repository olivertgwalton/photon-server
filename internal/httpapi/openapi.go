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

// optionalBody is a JSON body a client may leave out.
type optionalBody struct{ of any }

// asFile is a body or reply that is a file, by the types it may be.
type asFile []string

// asStream is a reply of Server-Sent Events, by each event's name and the JSON its data carries.
type asStream map[string]any

var (
	pageParams = []param{
		{"offset", 0, "Where the page starts, from 0."},
		{"limit", 0, "How many to answer, from 1 to " + strconv.Itoa(maxWallLimit) + "."},
	}
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

// An OpenAPI description's objects declare their fields in the order of their names, so each is
// written as a map of them would be.
type (
	apiDocument struct {
		Components apiComponents       `json:"components"`
		Info       apiInfo             `json:"info"`
		OpenAPI    string              `json:"openapi"`
		Paths      map[string]pathItem `json:"paths"`
	}
	apiInfo struct {
		Description string     `json:"description"`
		License     apiLicense `json:"license"`
		Title       string     `json:"title"`
		Version     string     `json:"version"`
	}
	apiLicense struct {
		Identifier string `json:"identifier"`
		Name       string `json:"name"`
	}
	apiComponents struct {
		Responses       map[string]apiResponse    `json:"responses"`
		Schemas         map[string]jsonSchema     `json:"schemas"`
		SecuritySchemes map[string]securityScheme `json:"securitySchemes"`
	}
	securityScheme struct {
		Description string `json:"description"`
		In          string `json:"in,omitempty"`
		Name        string `json:"name,omitempty"`
		Scheme      string `json:"scheme,omitempty"`
		Type        string `json:"type"`
	}
	// pathItem is a path's operations, by lower-case method.
	pathItem     map[string]apiOperation
	apiOperation struct {
		Description string                 `json:"description,omitempty"`
		Parameters  []apiParameter         `json:"parameters,omitempty"`
		RequestBody *requestBody           `json:"requestBody,omitempty"`
		Responses   map[string]apiResponse `json:"responses"`
		// Security is the schemes any one of which admits a request; none is a public route.
		Security []map[string][]string `json:"security"`
		Summary  string                `json:"summary"`
		Access   access                `json:"x-access"`
	}
	apiParameter struct {
		Description string     `json:"description,omitempty"`
		In          string     `json:"in"`
		Name        string     `json:"name"`
		Required    bool       `json:"required,omitempty"`
		Schema      jsonSchema `json:"schema"`
	}
	requestBody struct {
		Content  map[string]mediaType `json:"content"`
		Required bool                 `json:"required,omitempty"`
	}
	apiResponse struct {
		Ref         string               `json:"$ref,omitempty"`
		Content     map[string]mediaType `json:"content,omitempty"`
		Description string               `json:"description,omitempty"`
	}
	mediaType struct {
		Schema *jsonSchema `json:"schema,omitempty"`
		// Events are a stream's events, by name, as the JSON each one's data carries.
		Events map[string]jsonSchema `json:"x-events,omitempty"`
	}
)

// describe answers the API's OpenAPI description, from its routes.
func describe(info domain.Info, routes []route) ([]byte, error) {
	s := newSchemas()
	problemRef := s.of(reflect.TypeFor[problem]())
	paths := map[string]pathItem{}
	for _, r := range routes {
		if r.access == localNetwork {
			continue
		}
		method, path, _ := strings.Cut(r.pattern, " ")
		op, err := operation(s, r, path)
		if err != nil {
			return nil, err
		}
		if paths[path] == nil {
			paths[path] = pathItem{}
		}
		paths[path][strings.ToLower(method)] = op
	}
	if s.err != nil {
		return nil, s.err
	}
	return json.Marshal(apiDocument{
		OpenAPI: "3.1.1",
		Info: apiInfo{
			Title:   "photon-server",
			Version: info.Version,
			License: apiLicense{Name: "GPL-3.0-only", Identifier: "GPL-3.0-only"},
			Description: "A media server's API. Every refusal is a problem (RFC 9457) whose code says which. " +
				"A route marked admin answers only an admin's session. A signed address, as play answers, " +
				"is fetched as given, with no token: the signature is in its exp and sig, or in its path.",
		},
		Paths: paths,
		Components: apiComponents{
			Schemas: s.defs,
			Responses: map[string]apiResponse{
				"Problem": {
					Description: "A refusal.",
					Content:     map[string]mediaType{"application/problem+json": {Schema: &problemRef}},
				},
			},
			SecuritySchemes: map[string]securityScheme{
				"session": {
					Type: "http", Scheme: "bearer",
					Description: "A device's token, from signing in or pairing, in the Authorization header.",
				},
				"cookie": {
					Type: "apiKey", In: "cookie", Name: sessionCookie,
					Description: "The web app's session: the same token, kept by the browser. " +
						"A write carrying it is refused as forbidden unless it comes from this server's own pages.",
				},
			},
		},
	})
}

func operation(s *schemas, r route, path string) (apiOperation, error) {
	if r.summary == "" || r.status == 0 {
		return apiOperation{}, fmt.Errorf("%s has no summary or status", r.pattern)
	}
	if (r.reply == nil) != (r.status == http.StatusNoContent || r.status == http.StatusAccepted) {
		return apiOperation{}, fmt.Errorf("%s answers %d with reply %T", r.pattern, r.status, r.reply)
	}
	op := apiOperation{Summary: r.summary, Access: r.access, Security: []map[string][]string{}}
	for _, m := range wildcard.FindAllStringSubmatch(path, -1) {
		p := param{m[1], uuid.UUID{}, ""}
		if i := slices.IndexFunc(r.path, func(p param) bool { return p.name == m[1] }); i >= 0 {
			p = r.path[i]
		}
		op.Parameters = append(op.Parameters, parameter(s, p, "path"))
	}
	for _, p := range r.path {
		if !strings.Contains(path, "{"+p.name+"}") {
			return apiOperation{}, fmt.Errorf("%s has no wildcard %s", r.pattern, p.name)
		}
	}
	for _, p := range r.query {
		op.Parameters = append(op.Parameters, parameter(s, p, "query"))
	}
	switch r.access {
	case signedIn, admin, manages:
		op.Security = []map[string][]string{{"session": {}}, {"cookie": {}}}
	case public, signedAddress, signedPath, localNetwork:
	}
	switch r.access {
	case admin:
		op.Description = "Admin only."
	case manages:
		op.Description = "Admin, or a manager over the profiles it keeps."
	case public, signedIn, signedAddress, signedPath, localNetwork:
	}
	op.RequestBody = describeBody(s, r.body)
	success := apiResponse{Description: http.StatusText(r.status), Content: describeReply(s, r.reply)}
	op.Responses = map[string]apiResponse{
		strconv.Itoa(r.status): success,
		"default":              {Ref: "#/components/responses/Problem"},
	}
	if r.again != 0 {
		op.Responses[strconv.Itoa(r.again)] = apiResponse{Description: http.StatusText(r.again), Content: success.Content}
	}
	for status, refusal := range r.refusals {
		schema := s.of(reflect.TypeOf(refusal))
		op.Responses[strconv.Itoa(status)] = apiResponse{
			Description: http.StatusText(status),
			Content:     map[string]mediaType{"application/problem+json": {Schema: &schema}},
		}
	}
	return op, nil
}

// describeBody is what a route takes: none, a file, or JSON a client must send or may leave out.
func describeBody(s *schemas, of any) *requestBody {
	switch of := of.(type) {
	case nil:
		return nil
	case asFile:
		c := map[string]mediaType{}
		for _, t := range of {
			c[t] = mediaType{Schema: &jsonSchema{Type: schemaType{name: "string"}, ContentMediaType: t}}
		}
		return &requestBody{Required: true, Content: c}
	case optionalBody:
		schema := s.of(reflect.TypeOf(of.of))
		return &requestBody{Content: map[string]mediaType{"application/json": {Schema: &schema}}}
	default:
		schema := s.of(reflect.TypeOf(of))
		return &requestBody{Required: true, Content: map[string]mediaType{"application/json": {Schema: &schema}}}
	}
}

// describeReply is what a route answers on success: nothing, a file, a stream of events, or JSON.
func describeReply(s *schemas, reply any) map[string]mediaType {
	switch reply := reply.(type) {
	case nil:
		return nil
	case asFile:
		c := map[string]mediaType{}
		for _, t := range reply {
			c[t] = mediaType{}
		}
		return c
	case asStream:
		events := map[string]jsonSchema{}
		for name, data := range reply {
			events[name] = s.of(reflect.TypeOf(data))
		}
		return map[string]mediaType{"text/event-stream": {
			Schema: &jsonSchema{Type: schemaType{name: "string"}}, Events: events,
		}}
	default:
		schema := s.of(reflect.TypeOf(reply))
		return map[string]mediaType{"application/json": {Schema: &schema}}
	}
}

func parameter(s *schemas, p param, in string) apiParameter {
	return apiParameter{Name: p.name, In: in, Schema: s.of(reflect.TypeOf(p.of)), Required: in == "path", Description: p.doc}
}

func (a *API) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", openAPIType)
	_, _ = w.Write(a.description)
}

func (a *API) openAPIRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/openapi.json", access: public, summary: "Describe the API in OpenAPI 3.1",
			status: http.StatusOK, reply: asFile{openAPIType}, handle: a.openAPI,
		},
	}
}
