package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

func execCurl(req *http.Request) (*http.Response, error) {
	args := []string{
		"-s", // silent
		"-i", // include response headers
		"-L", // follow redirects
		"-X", req.Method,
	}

	for key, values := range req.Header {
		for _, v := range values {
			args = append(args, "-H", key+": "+v)
		}
	}

	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		args = append(args, "-d", string(body))
	}

	args = append(args, req.URL.String())

	cmd := exec.CommandContext(req.Context(), "curl", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("curl: %w", err)
	}

	return parseCurlResponse(out, req)
}

func parseCurlResponse(data []byte, req *http.Request) (*http.Response, error) {
	// With -L (follow redirects), curl output contains
	// multiple response blocks. Split on HTTP/ status
	// lines and use the last complete block.
	lines := strings.Split(string(data), "\n")

	var lastStatus string
	var lastHeaders []string
	var bodyStartLine int

	status := ""
	var headers []string
	inHeaders := false

	for i, line := range lines {
		line = strings.TrimRight(line, "\r")

		if strings.HasPrefix(line, "HTTP/") {
			status = line
			headers = nil
			inHeaders = true
			continue
		}

		if inHeaders && line == "" {
			lastStatus = status
			lastHeaders = headers
			bodyStartLine = i + 1
			inHeaders = false
			continue
		}

		if inHeaders {
			headers = append(headers, line)
		}
	}

	if lastStatus == "" {
		return nil, fmt.Errorf(
			"no valid HTTP response in curl output")
	}

	var bodyParts []string
	if bodyStartLine < len(lines) {
		bodyParts = lines[bodyStartLine:]
	}
	body := []byte(strings.Join(bodyParts, "\n"))

	resp := buildResponse(lastStatus, lastHeaders, body, req)
	if resp == nil {
		return nil, fmt.Errorf("failed to parse HTTP response")
	}
	return resp, nil
}

func buildResponse(
	statusLine string, headerLines []string,
	body []byte, req *http.Request,
) *http.Response {
	if statusLine == "" {
		return nil
	}

	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 2 {
		return nil
	}

	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil
	}

	resp := &http.Response{
		StatusCode: statusCode,
		Status:     statusLine,
		Proto:      parts[0],
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}

	for _, line := range headerLines {
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		resp.Header.Add(key, val)
	}

	return resp
}

func curlAvailable() bool {
	_, err := exec.LookPath("curl")
	return err == nil
}
