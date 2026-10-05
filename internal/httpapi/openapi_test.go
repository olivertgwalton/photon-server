package httpapi

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type describedParameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
}

type described struct {
	OpenAPI string `json:"openapi"`
	Paths   map[string]map[string]struct {
		Summary    string               `json:"summary"`
		Parameters []describedParameter `json:"parameters"`
		Responses  map[string]struct {
			Ref     string                     `json:"$ref"`
			Content map[string]json.RawMessage `json:"content"`
		} `json:"responses"`
	} `json:"paths"`
}

func fetchDescription(t *testing.T, a *API) []byte {
	t.Helper()
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != openAPIType {
		t.Errorf("Content-Type = %q, want %q", got, openAPIType)
	}
	return rec.Body.Bytes()
}

func TestTheDescriptionHasEveryRoute(t *testing.T) {
	a := newAPI(nil)
	var doc described
	if err := json.Unmarshal(fetchDescription(t, a), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.1.1" {
		t.Errorf("openapi = %q", doc.OpenAPI)
	}
	for _, r := range a.routes() {
		method, path, _ := strings.Cut(r.pattern, " ")
		op, ok := doc.Paths[path][strings.ToLower(method)]
		if !ok {
			t.Errorf("%s is not described", r.pattern)
			continue
		}
		has := func(name, in string) bool {
			return slices.ContainsFunc(op.Parameters, func(p describedParameter) bool {
				return p.Name == name && p.In == in && (in != "path" || p.Required)
			})
		}
		for _, m := range wildcard.FindAllStringSubmatch(path, -1) {
			if !has(m[1], "path") {
				t.Errorf("%s does not describe its path parameter %s", r.pattern, m[1])
			}
		}
		for _, q := range r.query {
			if !has(q.name, "query") {
				t.Errorf("%s does not describe its query parameter %s", r.pattern, q.name)
			}
		}
		success, ok := op.Responses[strconv.Itoa(r.status)]
		answersNothing := r.status == http.StatusNoContent || r.status == http.StatusAccepted
		if !ok || len(success.Content) == 0 && !answersNothing {
			t.Errorf("%s does not describe what it answers", r.pattern)
		}
		if op.Responses["default"].Ref != "#/components/responses/Problem" {
			t.Errorf("%s does not describe its refusals", r.pattern)
		}
	}
}

func TestTheDescriptionsReferencesResolve(t *testing.T) {
	var doc any
	if err := json.Unmarshal(fetchDescription(t, newAPI(nil)), &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				target := doc
				for part := range strings.SplitSeq(strings.TrimPrefix(ref, "#/"), "/") {
					m, _ := target.(map[string]any)
					target = m[part]
				}
				if target == nil {
					t.Errorf("%s resolves to nothing", ref)
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc)
}

type unlisted string

func TestARouteMustSayWhatItIs(t *testing.T) {
	tests := []struct {
		name  string
		route route
		want  string
	}{
		{"no summary", route{pattern: "GET /x", status: http.StatusOK, reply: Info{}}, "no summary"},
		{"no reply", route{pattern: "GET /x", summary: "x", status: http.StatusOK}, "reply"},
		{"a reply where none is sent", route{pattern: "GET /x", summary: "x", status: http.StatusNoContent, reply: Info{}}, "reply"},
		{
			"an enum with no values",
			route{pattern: "GET /x", summary: "x", status: http.StatusOK, reply: struct {
				Kind unlisted `json:"kind"`
			}{}},
			"no values",
		},
		{
			"a path parameter not in the path",
			route{pattern: "GET /x", summary: "x", status: http.StatusNoContent, path: []param{{"id", 0, ""}}},
			"no wildcard",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := describe(Info{}, []route{tt.route})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one saying %q", err, tt.want)
			}
		})
	}
}

type shadowed struct {
	Kind  string `json:"kind"`
	Other int    `json:"other"`
}

type shadowing struct {
	Kind bool `json:"kind"`
	shadowed
}

func TestASchemaHasTheFieldsEncodingJSONWrites(t *testing.T) {
	s := newSchemas()
	s.of(reflect.TypeFor[shadowing]())
	schema, _ := s.defs["Shadowing"].(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	if diff := cmp.Diff(map[string]any{"type": "boolean"}, props["kind"]); diff != "" {
		t.Errorf("kind (-want +got):\n%s", diff)
	}
	b, err := json.Marshal(shadowing{})
	if err != nil {
		t.Fatal(err)
	}
	var written map[string]any
	if err := json.Unmarshal(b, &written); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(slices.Sorted(maps.Keys(written)), slices.Sorted(maps.Keys(props))); diff != "" {
		t.Errorf("properties (-written +described):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"kind", "other"}, schema["required"]); diff != "" {
		t.Errorf("required (-want +got):\n%s", diff)
	}
}

// The description's code enum is problemCodes, so a code declared and not listed would be one no
// client is told of.
func TestEveryProblemCodeIsListed(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				v, _ := spec.(*ast.ValueSpec)
				if typ, ok := v.Type.(*ast.Ident); ok && typ.Name == "problemCode" {
					for _, value := range v.Values {
						lit, _ := value.(*ast.BasicLit)
						code, err := strconv.Unquote(lit.Value)
						if err != nil {
							t.Fatal(err)
						}
						declared = append(declared, code)
					}
				}
			}
		}
	}
	listed := values(problemCodes())
	slices.Sort(declared)
	slices.Sort(listed)
	if diff := cmp.Diff(declared, listed); diff != "" {
		t.Errorf("problem codes (-declared +listed):\n%s", diff)
	}
}
