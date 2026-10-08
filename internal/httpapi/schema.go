package httpapi

import (
	"cmp"
	"encoding"
	"encoding/json"
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
)

// enums are the values of each typed string the API sends or takes.
var enums = map[reflect.Type][]string{
	reflect.TypeFor[domain.AudioTrack]():          values(domain.AudioTracks()),
	reflect.TypeFor[domain.NextEpisode]():         values(domain.NextEpisodes()),
	reflect.TypeFor[domain.SegmentAction]():       values(domain.SegmentActions()),
	reflect.TypeFor[domain.SubtitleMode]():        values(domain.SubtitleModes()),
	reflect.TypeFor[domain.SubtitleDelivery]():    values(domain.SubtitleDeliveries()),
	reflect.TypeFor[domain.TrackMemory]():         values(domain.TrackMemories()),
	reflect.TypeFor[domain.Acceleration]():        values(domain.Accelerations()),
	reflect.TypeFor[domain.ArtworkKind]():         values(domain.ArtworkKinds()),
	reflect.TypeFor[domain.Capability]():          values(domain.Capabilities()),
	reflect.TypeFor[domain.CollectionOrigin]():    values(domain.CollectionOrigins()),
	reflect.TypeFor[domain.CollectionPlacement](): values(domain.CollectionPlacements()),
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
	reflect.TypeFor[domain.HEVCEncoding]():        values(domain.HEVCEncodings()),
	reflect.TypeFor[domain.RowVisibility]():       values(domain.RowVisibilities()),
	reflect.TypeFor[domain.HomeRow]():             values(domain.HomeRows()),
	reflect.TypeFor[domain.Availability]():        values(domain.Availabilities()),
	reflect.TypeFor[domain.CalendarFilter]():      values(domain.CalendarFilters()),
	reflect.TypeFor[domain.Milestone]():           values(domain.Milestones()),
	reflect.TypeFor[domain.Keep]():                values(domain.Keeps()),
	reflect.TypeFor[domain.SignInMethod]():        values(domain.SignInMethods()),
	reflect.TypeFor[domain.ItemKind]():            values(domain.ItemKinds()),
	reflect.TypeFor[domain.ImportSource]():        values(domain.ImportSources()),
	reflect.TypeFor[domain.ImportStatus]():        values(domain.ImportStatuses()),
	reflect.TypeFor[domain.ImportMiss]():          values(domain.ImportMisses()),
	reflect.TypeFor[domain.JobKind]():             values(domain.JobKinds()),
	reflect.TypeFor[domain.JobState]():            values(domain.JobStates()),
	reflect.TypeFor[domain.KeyframeMode]():        values(domain.KeyframeModes()),
	reflect.TypeFor[domain.MediaDeletion]():       values(domain.MediaDeletions()),
	reflect.TypeFor[domain.ArtworkLanguage]():     values(domain.ArtworkLanguages()),
	reflect.TypeFor[domain.TitleLanguage]():       values(domain.TitleLanguages()),
	reflect.TypeFor[domain.CollectionMode]():      values(domain.CollectionModes()),
	reflect.TypeFor[domain.SubtitleMatch]():       values(domain.SubtitleMatches()),
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
	reflect.TypeFor[domain.SearchKind]():          values(domain.SearchKinds()),
	reflect.TypeFor[domain.SegmentFormat]():       values(domain.SegmentFormats()),
	reflect.TypeFor[domain.ScanPhase]():           values(domain.ScanPhases()),
	reflect.TypeFor[domain.StreamKind]():          values(domain.StreamKinds()),
	reflect.TypeFor[domain.TaskKey]():             values(domain.TaskKeys()),
	reflect.TypeFor[domain.TaskResult]():          values(domain.TaskResults()),
	reflect.TypeFor[domain.Timing]():              values(domain.Timings()),
	reflect.TypeFor[domain.SecureConnections]():   values(domain.SecureConnectionModes()),
	reflect.TypeFor[domain.NodeAvailability]():    values(domain.NodeAvailabilities()),
	reflect.TypeFor[domain.NodeRole]():            values(domain.NodeRoles()),
	reflect.TypeFor[domain.LimitSource]():         values(domain.LimitSources()),
	reflect.TypeFor[domain.RestorePhase]():        values(domain.RestorePhases()),
	reflect.TypeFor[domain.RestoreResult]():       values(domain.RestoreResults()),
	reflect.TypeFor[domain.JellyfinMode]():        values(domain.JellyfinModes()),
	reflect.TypeFor[domain.StorageKind]():         values(domain.StorageKinds()),
	reflect.TypeFor[domain.Delivery]():            values(domain.Deliveries()),
	reflect.TypeFor[domain.ThemeLookup]():         values(domain.ThemeLookups()),
	reflect.TypeFor[domain.ThemeMusic]():          values(domain.ThemeMusics()),
	reflect.TypeFor[domain.Unrated]():             values(domain.UnratedPolicies()),
	reflect.TypeFor[domain.WallSort]():            values(domain.WallSorts()),
	reflect.TypeFor[domain.VideoCodec]():          values(domain.VideoCodecs()),
	reflect.TypeFor[domain.TranscodeReason]():     values(domain.TranscodeReasons()),
	reflect.TypeFor[downloadScope]():              values(downloadScopes()),
	reflect.TypeFor[decision]():                   values(decisions()),
	reflect.TypeFor[hiddenFolders]():              values(hiddenFolderModes()),
	reflect.TypeFor[problemCode]():                values(problemCodes()),
	reflect.TypeFor[rootAccess]():                 values(rootAccesses()),
	reflect.TypeFor[nodeReach]():                  values(nodeReaches()),
	reflect.TypeFor[subtitleFormat]():             values(subtitleFormats()),
	reflect.TypeFor[subtitleKind]():               values(subtitleKinds()),
}

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

// checkEnums refuses a value of an enum type in v, or anywhere in it, that is none of its values,
// so a handler never sees one. A field left out is empty and let be; an item of a list is not.
func checkEnums(v reflect.Value, item bool) error {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return checkEnums(v.Elem(), item)
		}
	case reflect.Struct:
		// A field the body cannot set is empty, so every one is walked and named only if refused.
		for i := range v.NumField() {
			err := checkEnums(v.Field(i), false)
			if err == nil {
				continue
			}
			f := v.Type().Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if e, ok := errors.AsType[*badEnum](err); ok && (!f.Anonymous || name != "") {
				e.path = strings.TrimSuffix(cmp.Or(name, f.Name)+"."+e.path, ".")
			}
			return err
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() < reflect.Array {
			return nil // numbers, as a uuid's bytes
		}
		for i := range v.Len() {
			if err := checkEnums(v.Index(i), true); err != nil {
				return err
			}
		}
	case reflect.Map:
		return checkMap(v)
	case reflect.String:
		list, ok := enums[v.Type()]
		s := v.String()
		if !ok || s == "" && !item || slices.Contains(list, s) || open[v.Type()] != nil && open[v.Type()].MatchString(s) {
			return nil
		}
		return &badEnum{typ: v.Type()}
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func,
		reflect.UnsafePointer:
	}
	return nil
}

// checkMap is apart from checkEnums so that only a map's value escapes to the heap.
func checkMap(m reflect.Value) error {
	for k, e := range m.Seq2() {
		if err := cmp.Or(checkEnums(k, true), checkEnums(e, true)); err != nil {
			return err
		}
	}
	return nil
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
	AdditionalProperties *jsonSchema           `json:"additionalProperties,omitempty"`
	AnyOf                []jsonSchema          `json:"anyOf,omitempty"`
	ContentMediaType     string                `json:"contentMediaType,omitempty"`
	Enum                 []string              `json:"enum,omitempty"`
	Format               string                `json:"format,omitempty"`
	Items                *jsonSchema           `json:"items,omitempty"`
	Pattern              string                `json:"pattern,omitempty"`
	Properties           map[string]jsonSchema `json:"properties,omitzero"`
	PropertyNames        *jsonSchema           `json:"propertyNames,omitempty"`
	Required             []string              `json:"required,omitempty"`
	Type                 schemaType            `json:"type,omitzero"`
}

// schemaType is a schema's type, written as its name, or with null beside it where it may be null.
type schemaType struct {
	name     string
	nullable bool
}

func (t schemaType) MarshalJSON() ([]byte, error) {
	if t.nullable {
		return json.Marshal([]string{t.name, "null"})
	}
	return json.Marshal(t.name)
}

// formats are the types that write themselves as a string.
var formats = map[reflect.Type]jsonSchema{
	reflect.TypeFor[time.Time]():   {Type: schemaType{name: "string"}, Format: "date-time"},
	reflect.TypeFor[uuid.UUID]():   {Type: schemaType{name: "string"}, Format: "uuid"},
	reflect.TypeFor[domain.Date](): {Type: schemaType{name: "string"}, Format: "date"},
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
	if t.Implements(marshaler) || t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(marshaler) {
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
				return jsonSchema{Type: schemaType{name: "string"}, AnyOf: []jsonSchema{{Enum: enum}, {Pattern: pattern.String()}}}
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
	props, required := map[string]jsonSchema{}, []string{}
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
