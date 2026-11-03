package httpapi

import (
	"cmp"
	"encoding"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/plugin"
	"github.com/olivertgwalton/photon-server/internal/tracker"
)

// enums are the values of each typed string the API sends or takes, and enumReader the option
// that refuses one none of its type's values as a request body is read, where it is.
var enums, enumReader = indexEnums(
	enumOf(feeds()),
	enumOf(domain.Countings()),
	enumOf(domain.AudioTracks()),
	enumOf(domain.NextEpisodes()),
	enumOf(domain.SegmentActions()),
	enumOf(domain.SubtitleModes()),
	enumOf(domain.SubtitleDeliveries()),
	enumOf(domain.TrackMemories()),
	enumOf(domain.Accelerations()),
	enumOf(domain.ArtworkKinds()),
	enumOf(domain.Capabilities()),
	enumOf(domain.LibraryMedias()),
	enumOf(domain.PluginProtocols()),
	enumOf(domain.CollectionOrigins()),
	enumOf(domain.CreditKinds()),
	enumOf(domain.Discoveries()),
	enumOf(domain.DolbyVisionHandlings()),
	enumOf(domain.DownloadStates()),
	enumOf(domain.EpisodeOrders()),
	enumOf(domain.RefreshModes()),
	enumOf(domain.ReleaseChecks()),
	enumOf(domain.EventKinds()),
	enumOf(domain.ExtraKinds()),
	enumOf(domain.Fields()),
	enumOf(domain.FieldSources()),
	enumOf(domain.HEVCEncodings()),
	enumOf(domain.RowVisibilities()),
	enumOf(domain.HomeRows()),
	enumOf(domain.Availabilities()),
	enumOf(domain.CalendarFilters()),
	enumOf(domain.Milestones()),
	enumOf(domain.Keeps()),
	enumOf(domain.SignInMethods()),
	enumOf(domain.ClientNetworks()),
	enumOf(domain.Provisionings()),
	enumOf(domain.Rechecks()),
	enumOf(domain.ItemKinds()),
	enumOf(domain.ImportSources()),
	enumOf(domain.ImportStatuses()),
	enumOf(domain.ImportMisses()),
	enumOf(domain.Trackers()),
	enumOf(domain.WebhookFormats()),
	enumOf(tracker.States()),
	enumOf(domain.JobKinds()),
	enumOf(domain.JobStates()),
	enumOf(domain.KeyframeModes()),
	enumOf(domain.MediaDeletions()),
	enumOf(domain.MissingVersions()),
	enumOf(domain.ArtworkLanguages()),
	enumOf(domain.TitleLanguages()),
	enumOf(domain.BoxSetModes()),
	enumOf(domain.RequestStates()),
	enumOf(domain.CollectionBoxSets()),
	enumOf(domain.SubtitleMatches()),
	enumOf(domain.LibraryKinds()),
	enumOf(domain.MarkerDetections()),
	enumOf(domain.MarkerKinds()),
	enumOf(domain.MarkerSources()),
	enumOf(domain.Marks()),
	enumOf(domain.Monitors()),
	enumOf(domain.Orders()),
	enumOf(domain.PartPlaybacks()),
	enumOf(domain.PlayMethods()),
	enumOf(domain.PreviewLevels()),
	enumOf(domain.PlayStates()),
	enumOf(domain.ProfileLocks()),
	enumOf(domain.Providers()),
	enumOf(domain.Ranges()),
	enumOf(domain.RatingSites()),
	enumOf(domain.Reaches()),
	enumOf(domain.Resolutions()),
	enumOf(domain.Roles()),
	enumOf(domain.SearchKinds()),
	enumOf(domain.SegmentFormats()),
	enumOf(domain.ScanPhases()),
	enumOf(domain.StreamKinds()),
	enumOf(domain.StoppedBys()),
	enumOf(domain.TaskKeys()),
	enumOf(plugin.PageAccesses()),
	enumOf(domain.TaskResults()),
	enumOf(domain.Timings()),
	enumOf(domain.SecureConnectionModes()),
	enumOf(domain.NodeAvailabilities()),
	enumOf(domain.NodeRoles()),
	enumOf(domain.LimitSources()),
	enumOf(domain.EncoderSources()),
	enumOf(domain.RestorePhases()),
	enumOf(domain.RestoreResults()),
	enumOf(domain.JellyfinModes()),
	enumOf(domain.StorageKinds()),
	enumOf(domain.Deliveries()),
	enumOf(domain.ThemeLookups()),
	enumOf(domain.ThemeMusics()),
	enumOf(domain.CardArts()),
	enumOf(domain.StillWatchings()),
	enumOf(domain.SpoilerModes()),
	enumOf(tracker.LinkFlows()),
	enumOf(domain.GroupKinds()),
	enumOf(domain.Notifies()),
	enumOf(domain.PushKinds()),
	enumOf(domain.NotificationKinds()),
	enumOf(domain.APNsEnvironments()),
	enumOf(domain.NotificationChanges()),
	enumOf(domain.UnratedPolicies()),
	enumOf(domain.WallSorts()),
	enumOf(domain.WallKinds()),
	enumOf(domain.VideoCodecs()),
	enumOf(domain.TranscodeReasons()),
	enumOf(downloadScopes()),
	enumOf(decisions()),
	enumOf(hiddenFolderModes()),
	enumOf(problemCodes()),
	enumOf(rootAccesses()),
	enumOf(setupStates()),
	enumOf(nodeReaches()),
	enumOf(subtitleFormats()),
	enumOf(subtitleKinds()),
)

// open are the enums that take, beside their own values, any matching a pattern: a registered
// plugin's source and the kind of id it files titles under.
var open = map[reflect.Type]*regexp.Regexp{
	reflect.TypeFor[domain.FieldSource](): regexp.MustCompile(domain.PluginPattern),
	reflect.TypeFor[domain.Provider]():    regexp.MustCompile(domain.PluginPattern),
}

// badEnum is a value of an enum type that is none of its values, at path in a request body.
type badEnum struct {
	path string
	typ  reflect.Type
}

func (e *badEnum) Error() string {
	msg := fmt.Sprintf("%s is one of %s", e.path, strings.Join(enums[e.typ], ", "))
	if _, ok := open[e.typ]; ok {
		msg += ", or a plugin's"
	}
	return msg
}

// enumType is a typed string's values, and the reader that refuses a request body's value of it
// that is none of them.
type enumType struct {
	typ    reflect.Type
	values []string
	read   *json.Unmarshalers
}

func enumOf[T ~string](list []T) enumType {
	t := reflect.TypeFor[T]()
	values := values(list)
	return enumType{t, values, json.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *T) error {
		if dec.PeekKind() != jsontext.KindString {
			return errors.ErrUnsupported
		}
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		s := tok.String()
		if !slices.Contains(values, s) && (s != "" || !omittable(dec)) && (open[t] == nil || !open[t].MatchString(s)) {
			return &badEnum{path: pathOf(dec), typ: t}
		}
		*v = T(s)
		return nil
	})}
}

func indexEnums(list ...enumType) (map[reflect.Type][]string, json.Options) {
	byType := make(map[reflect.Type][]string, len(list))
	read := make([]*json.Unmarshalers, len(list))
	for i, e := range list {
		if _, ok := byType[e.typ]; ok {
			panic(fmt.Sprintf("%v is listed among the enums twice", e.typ))
		}
		byType[e.typ], read[i] = e.values, e.read
	}
	return byType, json.WithUnmarshalers(json.JoinUnmarshalers(read...))
}

// omittable reports whether the value dec has just read may be empty: a field's, as a field left
// out is empty, but not an item of a list nor a map's key.
func omittable(dec *jsontext.Decoder) bool {
	kind, n := dec.StackIndex(dec.StackDepth())
	return kind != jsontext.KindBeginArray && (kind != jsontext.KindBeginObject || n%2 == 0)
}

// pathOf names where in a body the value dec has just read is, by the names it is under.
func pathOf(dec *jsontext.Decoder) string {
	var names []string
	depth := 0
	for name := range dec.StackPointer().Tokens() {
		depth++
		if kind, _ := dec.StackIndex(depth); kind == jsontext.KindBeginObject {
			names = append(names, name)
		}
	}
	return strings.Join(names, ".")
}

func values[T ~string](list []T) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = string(v)
	}
	return out
}

// jsonSchema is a JSON Schema, its fields in the order of their names.
type jsonSchema struct {
	Ref                  string                `json:"$ref,omitempty"`
	AdditionalProperties *jsonSchema           `json:"additionalProperties,omitzero"`
	AnyOf                []jsonSchema          `json:"anyOf,omitempty"`
	ContentMediaType     string                `json:"contentMediaType,omitempty"`
	Enum                 []string              `json:"enum,omitempty"`
	Format               string                `json:"format,omitempty"`
	Items                *jsonSchema           `json:"items,omitzero"`
	Pattern              string                `json:"pattern,omitempty"`
	Properties           map[string]jsonSchema `json:"properties,omitzero"`
	PropertyNames        *jsonSchema           `json:"propertyNames,omitzero"`
	Required             []string              `json:"required,omitempty"`
	Type                 schemaType            `json:"type,omitzero"`
}

// schemaType is a schema's type, written as its name, or with null beside it where it may be null.
type schemaType struct {
	name     string
	nullable bool
}

func (t schemaType) MarshalJSONTo(enc *jsontext.Encoder) error {
	if t.nullable {
		return json.MarshalEncode(enc, []string{t.name, "null"})
	}
	return enc.WriteToken(jsontext.String(t.name))
}

// uri is an absolute address; uriReference is one that may instead be a path on the server that
// answered, as a signed address is where a node has no public URL.
type (
	uri          string
	uriReference string
)

// formats are the types that write themselves: as a string, or a locator as the object a reader
// gave, of any properties.
var formats = map[reflect.Type]jsonSchema{
	reflect.TypeFor[jsontext.Value]():     {Type: schemaType{name: "object"}, AdditionalProperties: &jsonSchema{}},
	reflect.TypeFor[domain.LanguageTag](): {Type: schemaType{name: "string"}, Pattern: `^([A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*)?$`},
	reflect.TypeFor[domain.CountryCode](): {Type: schemaType{name: "string"}, Pattern: `^([A-Z]{2})?$`},
	reflect.TypeFor[uri]():                {Type: schemaType{name: "string"}, Format: "uri"},
	reflect.TypeFor[uriReference]():       {Type: schemaType{name: "string"}, Format: "uri-reference"},
	reflect.TypeFor[time.Time]():          {Type: schemaType{name: "string"}, Format: "date-time"},
	reflect.TypeFor[uuid.UUID]():          {Type: schemaType{name: "string"}, Format: "uuid"},
	reflect.TypeFor[domain.Date]():        {Type: schemaType{name: "string"}, Format: "date"},
}

// renamed are types whose names say too little beside the API's own.
var renamed = map[reflect.Type]string{
	reflect.TypeFor[playback.Profile](): "ClientProfile",
	reflect.TypeFor[domain.HomeRow]():   "HomeRowKind",
	reflect.TypeFor[tracker.State]():    "TrackerState",
	reflect.TypeFor[tracker.LinkFlow](): "TrackerLinkFlow",
}

// marshalers are the ways a type writes itself; v2 calls a pointer receiver's too.
var marshalers = []reflect.Type{
	reflect.TypeFor[json.Marshaler](),
	reflect.TypeFor[json.MarshalerTo](),
	reflect.TypeFor[encoding.TextMarshaler](),
}

// schemas builds JSON Schemas of types as encoding/json writes them, each named type once, by
// name, among defs. A field tagged omitempty or omitzero may be left out, and request types are
// tagged so too, though decoding ignores it.
type schemas struct {
	defs  map[string]jsonSchema
	named map[string]reflect.Type
	err   error
}

func newSchemas() *schemas {
	return &schemas{defs: map[string]jsonSchema{}, named: map[string]reflect.Type{}}
}

func (s *schemas) of(t reflect.Type) jsonSchema {
	if f, ok := formats[t]; ok {
		return f
	}
	if t.Kind() == reflect.Pointer {
		return nullable(s.of(t.Elem()))
	}
	if slices.ContainsFunc(marshalers, reflect.PointerTo(t).Implements) {
		s.fail(fmt.Errorf("%v writes itself and has no format", t))
		return jsonSchema{}
	}
	switch t.Kind() {
	case reflect.Struct:
		return s.ref(t, func() jsonSchema { return s.object(t) })
	case reflect.String:
		if t.PkgPath() == "" {
			return jsonSchema{Type: schemaType{name: "string"}}
		}
		enum, ok := enums[t]
		if !ok {
			s.fail(fmt.Errorf("%v has no values in enums", t))
			return jsonSchema{}
		}
		return s.ref(t, func() jsonSchema {
			if pattern, ok := open[t]; ok {
				return jsonSchema{Type: schemaType{name: "string"}, AnyOf: []jsonSchema{{Enum: enum}, {Type: schemaType{name: "string"}, Pattern: pattern.String()}}}
			}
			return jsonSchema{Type: schemaType{name: "string"}, Enum: enum}
		})
	case reflect.Slice, reflect.Array:
		items := s.of(t.Elem())
		return jsonSchema{Type: schemaType{name: "array"}, Items: &items}
	case reflect.Map:
		values := s.of(t.Elem())
		m := jsonSchema{Type: schemaType{name: "object"}, AdditionalProperties: &values}
		if t.Key().PkgPath() != "" {
			keys := s.of(t.Key())
			m.PropertyNames = &keys
		}
		return m
	case reflect.Bool:
		return jsonSchema{Type: schemaType{name: "boolean"}}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return jsonSchema{Type: schemaType{name: "integer"}}
	case reflect.Int64, reflect.Uint64:
		return jsonSchema{Type: schemaType{name: "integer"}, Format: "int64"}
	case reflect.Float32, reflect.Float64:
		return jsonSchema{Type: schemaType{name: "number"}}
	case reflect.Interface:
		if t == reflect.TypeFor[domain.EventDetails]() {
			// An object whose keys the event's kind says.
			return jsonSchema{Type: schemaType{name: "object"}, AdditionalProperties: &jsonSchema{}}
		}
		return jsonSchema{}
	case reflect.Invalid, reflect.Uintptr, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func,
		reflect.Pointer, reflect.UnsafePointer:
		s.fail(fmt.Errorf("%v cannot be described", t))
	}
	return jsonSchema{}
}

// ref describes a named type once among defs and answers a reference to it.
func (s *schemas) ref(t reflect.Type, describe func() jsonSchema) jsonSchema {
	if t.Name() == "" {
		return describe()
	}
	name := schemaName(t)
	ref := jsonSchema{Ref: "#/components/schemas/" + name}
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

func (s *schemas) object(t reflect.Type) jsonSchema {
	var all []jsonField
	collect(t, 0, &all)
	props := map[string]jsonSchema{}
	var required []string
	for _, f := range all {
		if !dominant(f, all) {
			continue
		}
		props[f.name] = s.of(f.typ)
		if f.required {
			required = append(required, f.name)
		}
	}
	return jsonSchema{Type: schemaType{name: "object"}, Properties: props, Required: required}
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

func nullable(schema jsonSchema) jsonSchema {
	if schema.Type.name != "" {
		schema.Type.nullable = true
		return schema
	}
	return jsonSchema{AnyOf: []jsonSchema{schema, {Type: schemaType{name: "null"}}}}
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
