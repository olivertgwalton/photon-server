package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// sidecarPlaying is fakePlaying with subtitle files of its own, by id, in root.
type sidecarPlaying struct {
	fakePlaying
	files map[uuid.UUID]sidecar
}

type sidecar struct {
	name, codec string
	lang        language.Tag
}

func (p sidecarPlaying) Subtitle(_ context.Context, id uuid.UUID) (store.SubtitleRef, error) {
	f, ok := p.files[id]
	if !ok {
		return store.SubtitleRef{}, store.ErrNotFound
	}
	return store.SubtitleRef{ID: id, Codec: f.codec, Language: f.lang}, nil
}

func (p sidecarPlaying) SubtitleFile(_ context.Context, id uuid.UUID) (string, string, error) {
	f, ok := p.files[id]
	if !ok {
		return "", "", store.ErrNotFound
	}
	return p.root, f.name, nil
}

func styled(line string) string {
	return "[Script Info]\r\nScriptType: v4.00+\r\n\r\n[Events]\r\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\r\n" +
		"Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,," + line + "\r\n"
}

// A styled subtitle file beside a copy written before UTF-8 is sent in UTF-8, read in its
// language's codepage, as JASSUB reads only UTF-8; one in UTF-8 is sent as it is.
func TestAStyledSubtitleFileIsSentInUTF8(t *testing.T) {
	root := t.TempDir()
	encoded := func(e encoding.Encoding, s string) []byte {
		b, err := e.NewEncoder().Bytes([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, tc := range []struct {
		name, want string
		lang       language.Tag
		file       []byte
	}{
		{"Western.fr.ass", styled("Café"), language.French, encoded(charmap.Windows1252, styled("Café"))},
		{"Cyrillic.ru.ass", styled("Привет"), language.Russian, encoded(charmap.Windows1251, styled("Привет"))},
		{"Japanese.ja.ass", styled("こんにちは"), language.Japanese, encoded(japanese.ShiftJIS, styled("こんにちは"))},
		{"Wide.en.ass", styled("Wide"), language.English, encoded(unicode.UTF16(unicode.LittleEndian, unicode.UseBOM), styled("Wide"))},
		{"Unicode.en.ass", styled("こんにちは"), language.English, []byte(styled("こんにちは"))},
	} {
		id := uuid.NewV7()
		if err := os.WriteFile(filepath.Join(root, tc.name), tc.file, 0o644); err != nil {
			t.Fatal(err)
		}
		signer := playback.NewSigner([]byte("key"))
		api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
			Auth: fakeAuth{}, Signer: signer,
			Playing: sidecarPlaying{fakePlaying{root: root}, map[uuid.UUID]sidecar{id: {tc.name, "ass", tc.lang}}},
		})
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, signer.Sign("/api/v1/subtitles/"+id.String()+"/file", time.Now().Add(time.Hour)), nil))
		if rec.Code != http.StatusOK || rec.Body.String() != tc.want || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/x-ssa") {
			t.Errorf("%s: %d %q %q, want it in UTF-8", tc.name, rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
		}
	}
}
