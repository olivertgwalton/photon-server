package words

import (
	"testing"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A client never shows one of the API's values by what it is sent as.
func TestEveryValueAClientShowsHasAName(t *testing.T) {
	w := In(language.English)
	hasNames(t, roles, domain.Roles())
	hasNames(t, markerKinds, domain.MarkerKinds())
	hasNames(t, extraKinds, domain.ExtraKinds())
	hasNames(t, importSources, domain.ImportSources())
	hasNames(t, importMisses, domain.ImportMisses())
	hasNames(t, trackers, domain.Trackers())
	hasNames(t, playMethods, domain.PlayMethods())
	hasNames(t, ratingSites, domain.RatingSites())
	hasNames(t, ranges, domain.Ranges())
	hasNames(t, resolutions, domain.Resolutions())
	hasNames(t, itemKinds, domain.ItemKinds())
	hasNames(t, libraryKinds, domain.LibraryKinds())
	hasNames(t, streamKinds, domain.StreamKinds())
	hasNames(t, marks, domain.Marks())
	hasNames(t, milestones, domain.Milestones())
	hasNames(t, calendarFilters, domain.CalendarFilters())
	hasNames(t, jobStates, domain.JobStates())
	hasNames(t, downloadStates, domain.DownloadStates())
	hasNames(t, accelerations, domain.Accelerations())
	hasNames(t, rows, domain.HomeRows())
	hasNames(t, jobKinds, domain.JobKinds())
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

func hasNames[T ~string](t *testing.T, names map[T]string, values []T) {
	t.Helper()
	for _, v := range values {
		if _, ok := names[v]; !ok {
			t.Errorf("%T %v has no name", v, v)
		}
	}
}
