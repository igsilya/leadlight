package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseCurlResponse_Simple(t *testing.T) {
	raw := "HTTP/1.1 200 OK\r\n" +
		"Content-Type: text/plain\r\n" +
		"X-Custom: lorem\r\n" +
		"\r\n" +
		"body content here"

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	resp, err := parseCurlResponse([]byte(raw), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "text/plain" {
		t.Errorf("Content-Type = %q",
			resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("X-Custom") != "lorem" {
		t.Errorf("X-Custom = %q",
			resp.Header.Get("X-Custom"))
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "body content here" {
		t.Errorf("body = %q", string(body))
	}
}

func TestParseCurlResponse_Redirect(t *testing.T) {
	raw := "HTTP/1.1 301 Moved Permanently\r\n" +
		"Location: https://example.com/new\r\n" +
		"\r\n" +
		"HTTP/1.1 200 OK\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"final body"

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	resp, err := parseCurlResponse([]byte(raw), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200 (final)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "final body" {
		t.Errorf("body = %q", string(body))
	}
}

func TestParseCurlResponse_404(t *testing.T) {
	raw := "HTTP/2 404 Not Found\r\n" +
		"Content-Type: application/json\r\n" +
		"\r\n" +
		`{"detail":"Not found."}`

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	resp, err := parseCurlResponse([]byte(raw), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
}

func TestParseCurlResponse_EmptyBody(t *testing.T) {
	raw := "HTTP/1.1 204 No Content\r\n" +
		"\r\n"

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	resp, err := parseCurlResponse([]byte(raw), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 204 {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("body = %q, want empty", string(body))
	}
}

func TestParseCurlResponse_Invalid(t *testing.T) {
	raw := "not http at all"
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	_, err := parseCurlResponse([]byte(raw), req)
	if err == nil {
		t.Error("expected error for invalid output")
	}
}

func TestCurlAvailable(t *testing.T) {
	if !curlAvailable() {
		t.Skip("curl not installed")
	}
}

func TestExecCurl_Real(t *testing.T) {
	if !curlAvailable() {
		t.Skip("curl not installed")
	}
	req, _ := http.NewRequest("GET",
		"https://patchwork.ozlabs.org/api/1.2/projects/47/", nil)
	req.Header.Set("User-Agent", "leadlight/1.0")
	req.Header.Set("Accept", "*/*")

	resp, err := execCurl(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "openvswitch") {
		t.Errorf("body doesn't contain openvswitch")
	}
}
