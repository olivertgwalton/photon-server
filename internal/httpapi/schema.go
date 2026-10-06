package httpapi

import (
	"cmp"
	"encoding"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

// enums are the values of each typed string the API sends or takes.
var enums = map[reflect.Type][]string{
	reflect.TypeFor[domain.Acceleration]():        values(domain.Accelerations()),
	reflect.TypeFor[domain.ArtworkKind]():         values(domain.ArtworkKinds()),
	reflect.TypeFor[domain.Capability]():          values(domain.Capabilities()),
	reflect.TypeFor[domain.CollectionOrigin]():    values(domain.CollectionOrigins()),
	reflect.TypeFor[domain.CreditKind]():          values(domain.CreditKinds()),
	reflect.TypeFor[domain.Discovery]():           values(domain.Discoveries()),
	reflect.TypeFor[domain.DolbyVisionHandling](): values(domain.DolbyVisionHandlings()),
	reflect.TypeFor[domain.DownloadState]():       values(domain.DownloadStates()),
	reflect.TypeFor[domain.EpisodeOrder]():        values(domain.EpisodeOrders()),
	reflect.TypeFor[domain.RefreshMode]():         values(domain.RefreshModes()),
	reflect.TypeFor[domain.EventKind]():           values(domain.EventKinds()),
	reflect.TypeFor[domain.ExtraKind]():           values(domain.ExtraKinds()),
	reflect.TypeFor[domain.Field]():               values(domain.Fields()),
	reflect.TypeFor[domain.FieldSource]():         values(domain.FieldSources()),
	reflect.TypeFor[domain.HomeRow]():             values(domain.HomeRows()),
	reflect.TypeFor[domain.Keep]():                values(domain.Keeps()),
	reflect.TypeFor[domain.ItemKind]():            values(domain.ItemKinds()),
	reflect.TypeFor[domain.JobKind]():             values(domain.JobKinds()),
	reflect.TypeFor[domain.JobState]():            values(domain.JobStates()),
	reflect.TypeFor[domain.KeyframeMode]():        values(domain.KeyframeModes()),
	reflect.TypeFor[domain.LibraryKind]():         values(domain.LibraryKinds()),
	reflect.TypeFor[domain.MarkerDetection]():     values(domain.MarkerDetections()),
	reflect.TypeFor[domain.MarkerKind]():          values(domain.MarkerKinds()),
	reflect.TypeFor[domain.MarkerSource]():        values(domain.MarkerSources()),
	reflect.TypeFor[domain.Mark]():                values(domain.Marks()),
	reflect.TypeFor[domain.Monitor]():             values(domain.Monitors()),
	reflect.TypeFor[domain.Order]():               values(domain.Orders()),
	reflect.TypeFor[domain.PartPlayback]():        values(domain.PartPlaybacks()),
	reflect.TypeFor[domain.PlayMethod]():          values(domain.PlayMethods()),
	reflect.TypeFor[domain.PreviewLevel]():        values(domain.PreviewLevels()),
	reflect.TypeFor[domain.PlayState]():           values(domain.PlayStates()),
	reflect.TypeFor[domain.ProfileLock]():         values(domain.ProfileLocks()),
	reflect.TypeFor[domain.Provider]():            values(domain.Providers()),
	reflect.TypeFor[domain.Range]():               values(domain.Ranges()),
	reflect.TypeFor[domain.RatingSite]():          values(domain.RatingSites()),
	reflect.TypeFor[domain.Reach]():               values(domain.Reaches()),
	reflect.TypeFor[domain.Resolution]():          values(domain.Resolutions()),
	reflect.TypeFor[domain.Role]():                values(domain.Roles()),
	reflect.TypeFor[domain.ScanPhase]():           values(domain.ScanPhases()),
	reflect.TypeFor[domain.StreamKind]():          values(domain.StreamKinds()),
	reflect.TypeFor[domain.TaskKey]():             values(domain.TaskKeys()),
	reflect.TypeFor[domain.TaskResult]():          values(domain.TaskResults()),
	reflect.TypeFor[domain.Unrated]():             values(domain.UnratedPolicies()),
	reflect.TypeFor[domain.WallSort]():            values(domain.WallSorts()),
	reflect.TypeFor[domain.TranscodeReason]():     values(domain.TranscodeReasons()),
	reflect.TypeFor[downloadScope]():              values(downloadScopes()),
	reflect.TypeFor[decision]():                   values(decisions()),
	reflect.TypeFor[hiddenFolders]():              values(hiddenFolderModes()),
	reflect.TypeFor[problemCode]():                values(problemCodes()),
	reflect.TypeFor[subtitleFormat]():             values(subtitleFormats()),
}

// open are the enums that take, beside their own values, any matching a pattern: a registered
// plugin's source and the kind of id it files titles under.
var open = map[reflect.Type]string{
	reflect.TypeFor[domain.FieldSource](): domain.PluginPattern,
	reflect.TypeFor[domain.Provider]():    domain.PluginPattern,
}

func values[T ~string](list []T) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = string(v)
	}
	return out
}

// formats are the types that write themselves as a string.
var formats = map[reflect.Type]map[string]any{
	reflect.TypeFor[time.Time]():   {"type": "string", "format": "date-time"},
	reflect.TypeFor[uuid.UUID]():   {"type": "string", "format": "uuid"},
	reflect.TypeFor[domain.Date](): {"type": "string", "format": "date"},
}

// renamed are types whose names say too little beside the API's own.
var renamed = map[reflect.Type]string{
	reflect.TypeFor[playback.Profile](): "ClientProfile",
	reflect.TypeFor[domain.HomeRow]():   "HomeRowKind",
}

var (
	marshaler     = reflect.TypeFor[json.Marshaler]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
)

// schemas builds JSON Schemas of types as encoding/json writes them, each named type once, by
// name, among defs. A field tagged omitempty or omitzero may be left out, and request types are
// tagged so too, though decoding ignores it.
type schemas struct {
	defs  map[string]any
	named map[string]reflect.Type
	err   error
}

func newSchemas() *schemas {
	return &schemas{defs: map[string]any{}, named: map[string]reflect.Type{}}
}

func (s *schemas) of(t reflect.Type) map[string]any {
	if f, ok := formats[t]; ok {
		return f
	}
	if t.Kind() == reflect.Pointer {
		return nullable(s.of(t.Elem()))
	}
	if t.Implements(marshaler) || t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(marshaler) {
		s.fail(fmt.Errorf("%v writes itself and has no format", t))
		return map[string]any{}
	}
	switch t.Kind() {
	case reflect.Struct:
		return s.ref(t, func() map[string]any { return s.object(t) })
	case reflect.String:
		if t.PkgPath() == "" {
			return map[string]any{"type": "string"}
		}
		enum, ok := enums[t]
		if !ok {
			s.fail(fmt.Errorf("%v has no values in enums", t))
			return map[string]any{}
		}
		return s.ref(t, func() map[string]any {
			if pattern, ok := open[t]; ok {
				return map[string]any{"type": "string", "anyOf": []any{map[string]any{"enum": enum}, map[string]any{"pattern": pattern}}}
			}
			return map[string]any{"type": "string", "enum": enum}
		})
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": s.of(t.Elem())}
	case reflect.Map:
		m := map[string]any{"type": "object", "additionalProperties": s.of(t.Elem())}
		if t.Key().PkgPath() != "" {
			m["propertyNames"] = s.of(t.Key())
		}
		return m
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer"}
	case reflect.Int64, reflect.Uint64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Invalid, reflect.Uintptr, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func,
		reflect.Pointer, reflect.UnsafePointer:
		s.fail(fmt.Errorf("%v cannot be described", t))
	}
	return map[string]any{}
}

// ref describes a named type once among defs and answers a reference to it.
func (s *schemas) ref(t reflect.Type, describe func() map[string]any) map[string]any {
	if t.Name() == "" {
		return describe()
	}
	name := schemaName(t)
	ref := map[string]any{"$ref": "#/components/schemas/" + name}
	if seen, ok := s.named[name]; ok {
		if seen != t {
			s.fail(fmt.Errorf("%v and %v are both %s: name one in renamed", seen, t, name))
		}
		return ref
	}
	s.named[name] = t
	s.defs[name] = describe()
	return ref
}

func (s *schemas) object(t reflect.Type) map[string]any {
	var all []jsonField
	collect(t, 0, &all)
	props, required := map[string]any{}, []string{}
	for _, f := range all {
		if !dominant(f, all) {
			continue
		}
		props[f.name] = s.of(f.typ)
		if f.required {
			required = append(required, f.name)
		}
	}
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

type jsonField struct {
	name     string
	typ      reflect.Type
	depth    int
	tagged   bool
	required bool
}

// collect lists a struct's fields as encoding/json sees them, an embedded struct's among them.
func collect(t reflect.Type, depth int, out *[]jsonField) {
	for f := range t.Fields() {
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			collect(f.Type, depth+1, out)
			continue
		}
		if !f.IsExported() {
			continue
		}
		*out = append(*out, jsonField{
			name: cmp.Or(name, f.Name), typ: f.Type, depth: depth, tagged: name != "",
			required: !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero"),
		})
	}
}

// dominant is whether encoding/json writes f among fields of the same name: the shallowest wins,
// then the one tagged with the name, and where that leaves more than one, none is written.
func dominant(f jsonField, all []jsonField) bool {
	for _, g := range all {
		if g == f || g.name != f.name {
			continue
		}
		if g.depth < f.depth || g.depth == f.depth && (g.tagged || !f.tagged) {
			return false
		}
	}
	return true
}

func (s *schemas) fail(err error) {
	if s.err == nil {
		s.err = err
	}
}

func nullable(schema map[string]any) map[string]any {
	if t, ok := schema["type"].(string); ok {
		n := maps.Clone(schema)
		n["type"] = []string{t, "null"}
		return n
	}
	return map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}
}

// schemaName is a type's name for a client: cardJSON is Card, and listJSON[cardJSON] CardList.
func schemaName(t reflect.Type) string {
	if name, ok := renamed[t]; ok {
		return name
	}
	name := t.Name()
	if base, arg, ok := strings.Cut(name, "["); ok {
		arg = strings.TrimSuffix(arg, "]")
		return exported(arg[strings.LastIndex(arg, ".")+1:]) + exported(base)
	}
	return exported(name)
}

func exported(name string) string {
	name = strings.TrimSuffix(name, "JSON")
	r, n := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[n:]
}
