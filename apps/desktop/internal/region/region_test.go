package region

import "testing"

func TestName(t *testing.T) {
	for code, want := range map[string]string{"jp": "Japan", "US": "United States", "gb": "United Kingdom", "zz": "ZZ", "": ""} {
		if got := Name(code); got != want {
			t.Errorf("Name(%q) = %q; want %q", code, got, want)
		}
	}
}

func TestLabel(t *testing.T) {
	for _, tc := range []struct{ name, code, want string }{
		{"a@icloud.com", "jp", "a@icloud.com · Japan"}, {"", "jp", "Japan"}, {"a@icloud.com", "", "a@icloud.com"}, {"", "", "id"},
	} {
		if got := Label(tc.name, tc.code, "id"); got != tc.want {
			t.Errorf("Label(%q, %q) = %q; want %q", tc.name, tc.code, got, tc.want)
		}
	}
}
