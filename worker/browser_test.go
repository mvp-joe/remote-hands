package worker

import (
	"testing"

	"github.com/go-rod/rod/lib/input"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// parseSelector Tests
// =============================================================================

func TestParseSelector_CSSDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector string
		want     string
	}{
		{"bare class", "button.primary", "button.primary"},
		{"id selector", "#submit", "#submit"},
		{"compound", "div > span", "div > span"},
		{"attribute", "[data-test]", "[data-test]"},
		{"pseudo-class", "button:hover", "button:hover"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			strategy, value := parseSelector(tt.selector)
			assert.Equal(t, "css", strategy)
			assert.Equal(t, tt.want, value)
		})
	}
}

func TestParseSelector_XPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector string
		wantVal  string
	}{
		{"simple xpath", "xpath=//div", "//div"},
		{"attribute xpath", `xpath=//button[@type="submit"]`, `//button[@type="submit"]`},
		{"text contains", "xpath=//span[contains(text(), 'Hello')]", "//span[contains(text(), 'Hello')]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			strategy, value := parseSelector(tt.selector)
			assert.Equal(t, "xpath", strategy)
			assert.Equal(t, tt.wantVal, value)
		})
	}
}

func TestParseSelector_Text(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector string
		wantVal  string
	}{
		{"simple text", "text=Submit", "Submit"},
		{"text with spaces", "text=Submit Order", "Submit Order"},
		{"text with regex chars", "text=Price: $10", "Price: $10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			strategy, value := parseSelector(tt.selector)
			assert.Equal(t, "text", strategy)
			assert.Equal(t, tt.wantVal, value)
		})
	}
}

func TestParseSelector_Role(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector string
		wantVal  string
	}{
		{"simple role", "role=button", "button"},
		{"role with name", `role=button[name="OK"]`, `button[name="OK"]`},
		{"textbox role", "role=textbox", "textbox"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			strategy, value := parseSelector(tt.selector)
			assert.Equal(t, "role", strategy)
			assert.Equal(t, tt.wantVal, value)
		})
	}
}

func TestParseSelector_Ref(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector string
		wantVal  string
	}{
		{"simple ref", "ref=e42", "e42"},
		{"larger ref", "ref=e12345", "e12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			strategy, value := parseSelector(tt.selector)
			assert.Equal(t, "ref", strategy)
			assert.Equal(t, tt.wantVal, value)
		})
	}
}

func TestParseSelector_UnknownPrefixFallsBackToCSS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector string
	}{
		{"unknown prefix", "foo=bar"},
		{"data attribute", "data-test=value"},
		{"css with equals", "input[type=text]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			strategy, value := parseSelector(tt.selector)
			assert.Equal(t, "css", strategy)
			assert.Equal(t, tt.selector, value)
		})
	}
}

func TestParseSelector_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		selector     string
		wantStrategy string
		wantValue    string
	}{
		{"empty string", "", "css", ""},
		{"equals at start", "=value", "css", "=value"},
		{"only equals", "=", "css", "="},
		{"prefix with empty value", "xpath=", "xpath", ""},
		{"multiple equals", "text=a=b=c", "text", "a=b=c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			strategy, value := parseSelector(tt.selector)
			assert.Equal(t, tt.wantStrategy, strategy)
			assert.Equal(t, tt.wantValue, value)
		})
	}
}

// =============================================================================
// parseRoleSpec Tests
// =============================================================================

func TestParseRoleSpec_SimpleRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		spec     string
		wantRole string
		wantName string
	}{
		{"button", "button", ""},
		{"textbox", "textbox", ""},
		{"link", "link", ""},
		{"heading", "heading", ""},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			t.Parallel()
			role, name := parseRoleSpec(tt.spec)
			assert.Equal(t, tt.wantRole, role)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

func TestParseRoleSpec_RoleWithName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spec     string
		wantRole string
		wantName string
	}{
		{"double quotes", `button[name="OK"]`, "button", "OK"},
		{"single quotes", `button[name='Cancel']`, "button", "Cancel"},
		{"with spaces", `button[name="Click Me"]`, "button", "Click Me"},
		{"textbox name", `textbox[name="Email"]`, "textbox", "Email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			role, name := parseRoleSpec(tt.spec)
			assert.Equal(t, tt.wantRole, role)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

func TestParseRoleSpec_MalformedBrackets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spec     string
		wantRole string
		wantName string
	}{
		{"unclosed bracket", "button[name=OK", "button", ""},
		{"no name attr", "button[foo=bar]", "button", ""},
		{"empty brackets", "button[]", "button", ""},
		{"missing quotes", "button[name=OK]", "button", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			role, name := parseRoleSpec(tt.spec)
			assert.Equal(t, tt.wantRole, role)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

// =============================================================================
// screenshotFormatMapping Tests
// =============================================================================

func TestScreenshotFormatMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		format          remotehandsv1.ScreenshotFormat
		wantContentType string
	}{
		{"unspecified defaults to PNG", remotehandsv1.ScreenshotFormat_SCREENSHOT_FORMAT_UNSPECIFIED, "image/png"},
		{"PNG", remotehandsv1.ScreenshotFormat_SCREENSHOT_FORMAT_PNG, "image/png"},
		{"JPEG", remotehandsv1.ScreenshotFormat_SCREENSHOT_FORMAT_JPEG, "image/jpeg"},
		{"WebP", remotehandsv1.ScreenshotFormat_SCREENSHOT_FORMAT_WEBP, "image/webp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, contentType := screenshotFormatMapping(tt.format)
			assert.Equal(t, tt.wantContentType, contentType)
		})
	}
}

// =============================================================================
// requireSelector Tests
// =============================================================================

func TestRequireSelector_Present(t *testing.T) {
	t.Parallel()

	selector := "#button"
	action := &remotehandsv1.BrowserAction{
		Type:     remotehandsv1.BrowserActionType_BROWSER_ACTION_CLICK,
		Selector: &selector,
	}

	result, err := requireSelector(action)
	assert.NoError(t, err)
	assert.Equal(t, "#button", result)
}

func TestRequireSelector_Nil(t *testing.T) {
	t.Parallel()

	action := &remotehandsv1.BrowserAction{
		Type:     remotehandsv1.BrowserActionType_BROWSER_ACTION_CLICK,
		Selector: nil,
	}

	_, err := requireSelector(action)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "selector is required")
}

func TestRequireSelector_Empty(t *testing.T) {
	t.Parallel()

	empty := ""
	action := &remotehandsv1.BrowserAction{
		Type:     remotehandsv1.BrowserActionType_BROWSER_ACTION_CLICK,
		Selector: &empty,
	}

	_, err := requireSelector(action)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "selector is required")
}

// =============================================================================
// mapModifierToKey Tests
// =============================================================================

func TestMapModifierToKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mod     remotehandsv1.Modifier
		wantKey input.Key
	}{
		{"unspecified", remotehandsv1.Modifier_MODIFIER_UNSPECIFIED, 0},
		{"alt", remotehandsv1.Modifier_MODIFIER_ALT, input.AltLeft},
		{"control", remotehandsv1.Modifier_MODIFIER_CONTROL, input.ControlLeft},
		{"meta", remotehandsv1.Modifier_MODIFIER_META, input.MetaLeft},
		{"shift", remotehandsv1.Modifier_MODIFIER_SHIFT, input.ShiftLeft},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key := mapModifierToKey(tt.mod)
			assert.Equal(t, tt.wantKey, key)
		})
	}
}

// =============================================================================
// mapKeyName Tests
// =============================================================================

func TestMapKeyName_NamedKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyName string
		wantKey input.Key
	}{
		{"Enter", "Enter", input.Enter},
		{"Tab", "Tab", input.Tab},
		{"Escape", "Escape", input.Escape},
		{"Backspace", "Backspace", input.Backspace},
		{"Delete", "Delete", input.Delete},
		{"ArrowUp", "ArrowUp", input.ArrowUp},
		{"ArrowDown", "ArrowDown", input.ArrowDown},
		{"ArrowLeft", "ArrowLeft", input.ArrowLeft},
		{"ArrowRight", "ArrowRight", input.ArrowRight},
		{"Space", "Space", input.Space},
		{"Home", "Home", input.Home},
		{"End", "End", input.End},
		{"PageUp", "PageUp", input.PageUp},
		{"PageDown", "PageDown", input.PageDown},
		{"Insert", "Insert", input.Insert},
		{"F1", "F1", input.F1},
		{"F12", "F12", input.F12},
		{"Shift", "Shift", input.ShiftLeft},
		{"Control", "Control", input.ControlLeft},
		{"Alt", "Alt", input.AltLeft},
		{"Meta", "Meta", input.MetaLeft},
		{"CapsLock", "CapsLock", input.CapsLock},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key := mapKeyName(tt.keyName)
			assert.Equal(t, tt.wantKey, key)
		})
	}
}

func TestMapKeyName_SingleCharacters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyName string
		wantKey input.Key
	}{
		{"lowercase a", "a", input.KeyA},
		{"lowercase z", "z", input.KeyZ},
		{"uppercase A", "A", input.KeyA},
		{"uppercase Z", "Z", input.KeyZ},
		{"digit 0", "0", input.Digit0},
		{"digit 9", "9", input.Digit9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key := mapKeyName(tt.keyName)
			assert.Equal(t, tt.wantKey, key)
		})
	}
}

func TestMapKeyName_Unknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyName string
	}{
		{"unknown named key", "NotAKey"},
		{"empty string", ""},
		{"multi-char unknown", "abc"},
		{"symbol", "!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key := mapKeyName(tt.keyName)
			assert.Equal(t, input.Key(0), key)
		})
	}
}

// =============================================================================
// mapRuneToKey Tests
// =============================================================================

func TestMapRuneToKey_Letters(t *testing.T) {
	t.Parallel()

	// Test lowercase letters
	for r := 'a'; r <= 'z'; r++ {
		expected := input.KeyA + input.Key(r-'a')
		assert.Equal(t, expected, mapRuneToKey(r), "lowercase %c", r)
	}

	// Test uppercase letters
	for r := 'A'; r <= 'Z'; r++ {
		expected := input.KeyA + input.Key(r-'A')
		assert.Equal(t, expected, mapRuneToKey(r), "uppercase %c", r)
	}
}

func TestMapRuneToKey_Digits(t *testing.T) {
	t.Parallel()

	for r := '0'; r <= '9'; r++ {
		expected := input.Digit0 + input.Key(r-'0')
		assert.Equal(t, expected, mapRuneToKey(r), "digit %c", r)
	}
}

func TestMapRuneToKey_Whitespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		r       rune
		wantKey input.Key
	}{
		{"space", ' ', input.Space},
		{"tab", '\t', input.Tab},
		{"newline", '\n', input.Enter},
		{"carriage return", '\r', input.Enter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantKey, mapRuneToKey(tt.r))
		})
	}
}

func TestMapRuneToKey_Unmapped(t *testing.T) {
	t.Parallel()

	// Symbols and special characters return 0.
	unmapped := []rune{'!', '@', '#', '$', '%', '^', '&', '*', '(', ')', '-', '+', '=', '[', ']', '{', '}', '|', '\\', '/', '?', '<', '>', ',', '.', ';', ':', '\'', '"', '`', '~'}
	for _, r := range unmapped {
		assert.Equal(t, input.Key(0), mapRuneToKey(r), "symbol %c should return 0", r)
	}
}

// =============================================================================
// actionSuccess/actionError Tests
// =============================================================================

func TestActionSuccess_NoValue(t *testing.T) {
	t.Parallel()

	result := actionSuccess(nil)
	assert.True(t, result.Success)
	assert.Nil(t, result.Value)
	assert.Nil(t, result.Error)
}

func TestActionSuccess_WithValue(t *testing.T) {
	t.Parallel()

	val := `"hello"`
	result := actionSuccess(&val)
	assert.True(t, result.Success)
	assert.NotNil(t, result.Value)
	assert.Equal(t, `"hello"`, *result.Value)
	assert.Nil(t, result.Error)
}

func TestActionError(t *testing.T) {
	t.Parallel()

	result := actionError(assert.AnError)
	assert.False(t, result.Success)
	assert.Nil(t, result.Value)
	assert.NotNil(t, result.Error)
	assert.Equal(t, assert.AnError.Error(), *result.Error)
}

// =============================================================================
// pollUntil Tests
// =============================================================================

func TestPollUntil_ImmediateSuccess(t *testing.T) {
	t.Parallel()

	calls := 0
	err := pollUntil(t.Context(), 100*1e6, 10*1e6, func() error {
		calls++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestPollUntil_EventualSuccess(t *testing.T) {
	t.Parallel()

	calls := 0
	err := pollUntil(t.Context(), 500*1e6, 10*1e6, func() error {
		calls++
		if calls < 3 {
			return assert.AnError
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestPollUntil_Timeout(t *testing.T) {
	t.Parallel()

	err := pollUntil(t.Context(), 50*1e6, 10*1e6, func() error {
		return assert.AnError
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

// =============================================================================
// BrowserManager State Tests (no browser required)
// =============================================================================

func TestBrowserManager_NewBrowserManager(t *testing.T) {
	t.Parallel()

	bm := NewBrowserManager()
	assert.NotNil(t, bm)
	assert.NotNil(t, bm.pages)
	assert.NotNil(t, bm.refMap)
	assert.Empty(t, bm.pages)
	assert.Empty(t, bm.refMap)
	assert.False(t, bm.IsRunning())
}

func TestBrowserManager_IsRunning_WhenNotStarted(t *testing.T) {
	t.Parallel()

	bm := NewBrowserManager()
	assert.False(t, bm.IsRunning())
}

func TestBrowserManager_GetPage_NoPagesOpen(t *testing.T) {
	t.Parallel()

	bm := NewBrowserManager()
	_, _, err := bm.getPage(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no pages open")
}

func TestBrowserManager_GetPage_PageNotFound(t *testing.T) {
	t.Parallel()

	bm := NewBrowserManager()
	bm.lastPageID = "existing-page"
	// Don't actually add the page to bm.pages

	pageID := "nonexistent"
	_, _, err := bm.getPage(&pageID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// =============================================================================
// knownSelectorPrefixes Tests
// =============================================================================

func TestKnownSelectorPrefixes(t *testing.T) {
	t.Parallel()

	// Verify the expected prefixes are in the map.
	expected := []string{"xpath", "text", "role", "ref"}
	for _, prefix := range expected {
		assert.True(t, knownSelectorPrefixes[prefix], "expected %q to be a known prefix", prefix)
	}

	// css should NOT be in the map (it's the fallback).
	assert.False(t, knownSelectorPrefixes["css"], "css should not be a known prefix")
}
