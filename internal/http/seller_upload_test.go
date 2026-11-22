package web

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListingUploadRejectsSecondImage(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("title", "Preserved draft title"); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"first.png", "second.png"} {
		part, err := writer.CreateFormFile("image", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, "image fixture"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/sell/drafts", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	image, err := readListingSubmission(httptest.NewRecorder(), request)
	if len(image) != 0 || err == nil || err.Error() != "Upload one listing image at a time." {
		t.Fatalf("expected duplicate image rejection, got %d image bytes and %v", len(image), err)
	}
	if title := request.PostForm.Get("title"); title != "Preserved draft title" {
		t.Fatalf("expected submitted text to survive image rejection, got %q", title)
	}
}
