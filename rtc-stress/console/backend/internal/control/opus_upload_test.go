package control

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"rtcstress/internal/scenario"
	"testing"
)

func TestOpusUploadMetadataAndMode(t *testing.T) {
	store, err := NewAudioStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../worker/testdata/silence-20ms.opus")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "voice.opus")
	part.Write(data)
	form.Close()
	request := httptest.NewRequest("POST", "/api/audio", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	store.Upload(response, request)
	if response.Code != 201 {
		t.Fatal(response.Code, response.Body.String())
	}
	var file AudioFile
	if err := json.Unmarshal(response.Body.Bytes(), &file); err != nil {
		t.Fatal(err)
	}
	if file.Codec != "opus" || file.SampleRate != 48000 || file.Channels != 1 || file.FrameDurationMS != 20 || file.DurationSec != 0.02 {
		t.Fatalf("wrong Opus metadata: %+v", file)
	}
	c := scenario.Defaults()
	c.AudioChannels = 1
	c.AudioFiles = [2]string{"asset:" + file.ID, "builtin:660"}
	if _, err := store.Normalize(c); err == nil {
		t.Fatal("mixed PCM/Opus accepted")
	}
	c.AudioFiles[1] = "other.opus"
	normalized, err := store.Normalize(c)
	if err != nil || normalized.AudioInputMode != "opus" {
		t.Fatal("Opus mode not selected", err)
	}
}
