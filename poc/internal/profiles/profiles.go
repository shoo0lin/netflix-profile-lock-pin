// Package profiles holds the shared, standard-library-only pieces of the
// Profile Lock harnesses: cookie parsing, the pg GUID extraction, the one
// persisted-query POST, and the "your own account" guard.
//
// Nothing here talks to a browser. The two gate harnesses add a thin CDP
// driver in internal/browser; the read harness needs nothing but this file.
package profiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// GraphQLEndpoint is the endpoint the persisted operation is POSTed to.
const GraphQLEndpoint = "https://www.netflix.com/graphql"

// PersistedOp is the (name, id, version) triple the edge expects.
type PersistedOp struct {
	Name    string
	ID      string
	Version int
}

// LockTemplate is the picker query used by every harness in this repository.
var LockTemplate = PersistedOp{
	Name:    "ProfileLockTemplate",
	ID:      "d821963d-5bb5-43c7-a2d1-2cf7eab9f44e",
	Version: 102,
}

// UA is the browser identifier used for the raw POST.
const UA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

// Chrome is the browser binary the gate harnesses drive. CHROME_BIN overrides it.
func Chrome() string {
	if p := os.Getenv("CHROME_BIN"); p != "" {
		return p
	}
	return "/opt/google/chrome/chrome"
}

// Cookie is one name/value pair from an exported cookie line. Browser flags
// (domain, secure, httpOnly, sameSite) are applied by internal/browser.
type Cookie struct {
	Name  string
	Value string
}

// ReadCookieFile reads the one-line `k=v; k=v` string a browser exports.
func ReadCookieFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read cookie file %q: %w", path, err)
	}
	return strings.TrimSpace(string(b)), nil
}

// ParseCookies turns the exported string into individual cookies. Deliberately
// splits on "; " to match the Python harness exactly.
func ParseCookies(cookieString string) []Cookie {
	var out []Cookie
	for _, part := range strings.Split(cookieString, "; ") {
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out = append(out, Cookie{Name: name, Value: value})
	}
	return out
}

var pgRe = regexp.MustCompile(`pg%3D([A-Z0-9]+)|pg=([A-Z0-9]+)`)

// PGOfNetflixID extracts the active profile GUID from a NetflixId cookie value.
// The GUID is stored as a `pg=` (or URL-encoded `pg%3D`) parameter inside it.
// An empty string means "not present", matching Python's None at call sites.
func PGOfNetflixID(value string) string {
	m := pgRe.FindStringSubmatch(value)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

// PGOfCookieString extracts the same GUID from a whole exported cookie line.
func PGOfCookieString(cookieString string) string {
	for _, c := range ParseCookies(cookieString) {
		if c.Name == "NetflixId" {
			return PGOfNetflixID(c.Value)
		}
	}
	return ""
}

// PGOfCookies extracts the GUID from cookies read back out of a browser.
func PGOfCookies(cookies []Cookie) string {
	for _, c := range cookies {
		if c.Name == "NetflixId" {
			return PGOfNetflixID(c.Value)
		}
	}
	return ""
}

// GraphResponse is the raw result of one persisted-query POST.
type GraphResponse struct {
	Status  int
	Headers http.Header
	Body    string
}

// Post POSTs one persisted operation. It returns the status and headers even
// on a non-2xx answer, so the caller can print an UNAUTHENTICATED control
// exactly as it arrived.
func Post(op PersistedOp, variables any, cookieString string) (GraphResponse, error) {
	body, err := json.Marshal(struct {
		OperationName string         `json:"operationName"`
		Variables     any            `json:"variables"`
		Extensions    map[string]any `json:"extensions"`
	}{
		OperationName: op.Name,
		Variables:     variables,
		Extensions: map[string]any{
			"persistedQuery": map[string]any{"id": op.ID, "version": op.Version},
		},
	})
	if err != nil {
		return GraphResponse{}, err
	}

	req, err := http.NewRequest(http.MethodPost, GraphQLEndpoint, bytes.NewReader(body))
	if err != nil {
		return GraphResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://www.netflix.com")
	req.Header.Set("Referer", "https://www.netflix.com/")
	req.Header.Set("User-Agent", UA)
	req.Header.Set("Cookie", cookieString)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return GraphResponse{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return GraphResponse{}, err
	}
	return GraphResponse{Status: resp.StatusCode, Headers: resp.Header, Body: string(raw)}, nil
}

// Pin masks a string PIN: presence in the response is the finding.
func Pin(value string) string {
	if value == "" {
		return "null"
	}
	return fmt.Sprintf("set (%d chars)", utf8.RuneCountInString(value))
}

// PinValue is Pin for a JSON value, so nil/false/0 print `null` exactly as the
// Python `if not value` guard did.
func PinValue(value any) string {
	if !PyTruth(value) {
		return "null"
	}
	return Pin(PyString(value))
}

// PyString reproduces the handful of Python str() cases the transcripts use.
func PyString(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case int:
		return strconv.Itoa(v)
	case json.Number:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// PyTruth reproduces Python truthiness for the JSON values we inspect.
func PyTruth(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0
	case int:
		return v != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}

// Truncate is Python's `str(value)[:n]`, counting code points.
func Truncate(value any, n int) string {
	s := []rune(PyString(value))
	if len(s) > n {
		s = s[:n]
	}
	return string(s)
}

// PyBool renders a JSON value the way Python's f-string would.
func PyBool(value any) string {
	return PyString(value)
}

// RequireOwnAccount is the one rule that matters: these harnesses act on the
// account whose cookies the caller supplies. Python's sys.exit(string) exits 1.
func RequireOwnAccount(enabled bool) {
	if enabled {
		return
	}
	fmt.Fprintln(os.Stderr, "refusing to run without --confirm-own-account")
	fmt.Fprintln(os.Stderr, "These harnesses act on the account whose cookies you supply. Use your")
	fmt.Fprintln(os.Stderr, "own account, your own profiles, and put the PIN back when you are done.")
	os.Exit(1)
}
