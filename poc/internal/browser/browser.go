// Package browser is the thin CDP layer the two gate harnesses need. The read
// harness has no browser dependency at all; this package is only used to drive
// /ProfilesGate and read the session's cookies back after a transition.
//
// Behaviour mirrors the old Playwright harnesses: a persistent Chromium profile,
// the exported cookies injected for .netflix.com, the first existing tab reused,
// and the same selector families and timeouts.
package browser

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/shoo0lin/netflix-profile-lock-pin/poc/internal/profiles"
)

const gateURL = "https://www.netflix.com/ProfilesGate"

// PromptFields is the old PROMPT_FIELDS tuple, in the same priority order.
var PromptFields = []string{
	`input[inputmode="numeric"]`,
	`input[type="tel"]`,
	`input[type="password"]`,
	`input[name="pin"]`,
}

// Session owns the launched Chromium and the page used by a gate harness.
type Session struct {
	Browser *rod.Browser
	Page    *rod.Page
}

// Open launches Chromium with a persistent profile, injects the exported
// cookies, and reuses the first tab (or creates one), like
// launch_persistent_context did in the Python harnesses.
func Open(profileDir string, headed bool, cookieString string) (*Session, error) {
	u, err := launcher.New().
		Bin(profiles.Chrome()).
		UserDataDir(profileDir).
		Headless(!headed).
		NoSandbox(true).
		Set("disable-blink-features", "AutomationControlled").
		Launch()
	if err != nil {
		return nil, err
	}

	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		_ = browser.Close()
		return nil, err
	}

	pages, err := browser.Pages()
	if err != nil {
		_ = browser.Close()
		return nil, err
	}
	var page *rod.Page
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = browser.Page(proto.TargetCreateTarget{})
		if err != nil {
			_ = browser.Close()
			return nil, err
		}
	}

	if err := browser.SetCookies(cookieParams(cookieString)); err != nil {
		_ = browser.Close()
		return nil, err
	}
	return &Session{Browser: browser, Page: page}, nil
}

// Close releases the browser. Callers defer it once the work is done.
func (s *Session) Close() error {
	if s == nil || s.Browser == nil {
		return nil
	}
	return s.Browser.Close()
}

// CookieParams builds the same cookie dicts the Python harness handed to
// add_cookies: .netflix.com, path /, secure, httpOnly for the identity cookies,
// and SameSite=None.
func cookieParams(cookieString string) []*proto.NetworkCookieParam {
	var out []*proto.NetworkCookieParam
	for _, c := range profiles.ParseCookies(cookieString) {
		out = append(out, &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   ".netflix.com",
			Path:     "/",
			Secure:   true,
			HTTPOnly: c.Name == "NetflixId" || c.Name == "SecureNetflixId",
			SameSite: proto.NetworkCookieSameSiteNone,
		})
	}
	return out
}

// Cookies reads the browser's cookies back, for pg extraction before/after.
func (s *Session) Cookies() ([]profiles.Cookie, error) {
	got, err := s.Browser.GetCookies()
	if err != nil {
		return nil, err
	}
	out := make([]profiles.Cookie, 0, len(got))
	for _, c := range got {
		out = append(out, profiles.Cookie{Name: c.Name, Value: c.Value})
	}
	return out, nil
}

// PG returns the active profile GUID from the browser's current cookies.
func (s *Session) PG() (string, error) {
	cookies, err := s.Cookies()
	if err != nil {
		return "", err
	}
	return profiles.PGOfCookies(cookies), nil
}

// GotoGate navigates with the same 60s timeout the Python harness used, and
// waits for the document to be ready.
func (s *Session) GotoGate() error {
	p := s.Page.Timeout(60 * time.Second)
	if err := p.Navigate(gateURL); err != nil {
		return err
	}
	return p.WaitLoad()
}

// Wait pauses for the given number of milliseconds (Python wait_for_timeout).
func Wait(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// Count returns the number of elements matching selector.
func (s *Session) Count(selector string) (int, error) {
	els, err := s.Page.Elements(selector)
	if err != nil {
		return 0, err
	}
	return len(els), nil
}

// ClickFirst clicks the first match, with a bounded timeout.
func (s *Session) ClickFirst(selector string, timeout time.Duration) error {
	els, err := s.Page.Elements(selector)
	if err != nil {
		return err
	}
	if len(els) == 0 {
		return fmt.Errorf("no element for selector %q", selector)
	}
	return els[0].Timeout(timeout).Click(proto.InputMouseButtonLeft, 1)
}

// FindPrompt returns the first prompt-field selector present on the page, or
// "" when none of the known fields exist.
func (s *Session) FindPrompt() (string, error) {
	for _, sel := range PromptFields {
		n, err := s.Count(sel)
		if err != nil {
			return "", err
		}
		if n > 0 {
			return sel, nil
		}
	}
	return "", nil
}

// AnyPrompt reports whether any known prompt field is present.
func (s *Session) AnyPrompt() (bool, error) {
	sel, err := s.FindPrompt()
	return sel != "", err
}

// Fill sets the PIN in the first matching prompt field, emulating fill().
func (s *Session) Fill(selector, value string) error {
	els, err := s.Page.Elements(selector)
	if err != nil {
		return err
	}
	if len(els) == 0 {
		return fmt.Errorf("no element for selector %q", selector)
	}
	el := els[0]
	if err := el.SelectAllText(); err != nil {
		return err
	}
	return el.Input(value)
}

// Submit tries the old Playwright selector family in order: buttons whose text
// contains Unlock, Continue or Submit (case-insensitively), then any submit
// button. A click failure moves on to the next candidate, as the old harness
// did with its try/except.
func (s *Session) Submit() error {
	for _, label := range []string{"Unlock", "Continue", "Submit"} {
		buttons, err := s.Page.Elements("button")
		if err != nil {
			return err
		}
		for _, b := range buttons {
			text, err := b.Text()
			if err != nil {
				continue
			}
			if !strings.Contains(strings.ToLower(text), strings.ToLower(label)) {
				continue
			}
			if err := b.Timeout(5 * time.Second).Click(proto.InputMouseButtonLeft, 1); err == nil {
				return nil
			}
		}
	}
	els, err := s.Page.Elements(`button[type="submit"]`)
	if err != nil {
		return err
	}
	if len(els) == 0 {
		return fmt.Errorf("no submit button found")
	}
	return els[0].Timeout(5 * time.Second).Click(proto.InputMouseButtonLeft, 1)
}

// Location returns the page URL and title, matching page.url / page.title().
func (s *Session) Location() (string, string, error) {
	info, err := s.Page.Info()
	if err != nil {
		return "", "", err
	}
	return info.URL, info.Title, nil
}
