package words

import (
	"testing"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A client never shows one of the API's values by what it is sent as.
func TestEveryValueAClientShowsHasAName(t *testing.T) {
	w := In(language.English)
	for _, list := range [][]any{
		each(domain.Roles()),
		each(domain.MarkerKinds()),
		each(domain.ExtraKinds()),
		each(domain.ImportSources()),
		each(domain.ImportMisses()),
		each(domain.PlayMethods()),
		each(domain.RatingSites()),
		each(domain.Ranges()),
		each(domain.Resolutions()),
		each(domain.ItemKinds()),
		each(domain.LibraryKinds()),
		each(domain.StreamKinds()),
		each(domain.Marks()),
		each(domain.Milestones()),
		each(domain.CalendarFilters()),
		each(domain.JobStates()),
		each(domain.DownloadStates()),
		each(domain.Accelerations()),
		each(domain.HomeRows()),
		each(domain.JobKinds()),
	} {
		for _, v := range list {
			if _, ok := names[v]; !ok {
				t.Errorf("%T %v has no name", v, v)
			}
		}
	}
	for _, k := range domain.TaskKeys() {
		if d := w.Task(k); d.Name == string(k) || d.Description == "" {
			t.Errorf("task %s is named %+v", k, d)
		}
	}
	for _, r := range domain.NodeRoles() {
		if d := w.NodeRole(r); d.Name == "" || d.Description == "" {
			t.Errorf("node role %s is named %+v", r, d)
		}
	}
	for _, r := range domain.TranscodeReasons() {
		if d := w.TranscodeReason(r); d.Name == "" || d.Description == "" {
			t.Errorf("transcode reason %s is named %+v", r, d)
		}
	}
	for _, s := range domain.WallSorts() {
		if d := w.Sort(s); d.Name == "" || d.Ascending == "" || d.Descending == "" {
			t.Errorf("sort %s is named %+v", s, d)
		}
	}
	for _, k := range domain.LoggedEventKinds() {
		if w.Kept(k) == "" {
			t.Errorf("logged event %s has no name", k)
		}
	}
	for _, k := range domain.HookableEventKinds() {
		if w.Told(k) == "" {
			t.Errorf("hookable event %s has no name", k)
		}
	}
}

func each[T any](values []T) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
