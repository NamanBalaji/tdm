package bencode_test

import (
	"reflect"
	"testing"

	"github.com/NamanBalaji/tdm/internal/torrent/bencode"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      any
		wantError bool
	}{
		{"string-simple", "4:spam", "spam", false},
		{"string-empty", "0:", "", false},
		{"string-missing-colon", "4spam", nil, true},
		{"string-too-short", "5:spam", nil, true},
		{"string-negative-len", "-1:foo", nil, true},

		{"int-positive", "i42e", 42, false},
		{"int-zero", "i0e", 0, false},
		{"int-negative", "i-3e", -3, false},
		{"int-no-e", "i42", nil, true},
		{"int-missing-i", "42e", nil, true},
		{"int-bad-digits", "i4.2e", nil, true},

		{"list-empty", "le", []any(nil), false},
		{"list-mixed", "l4:spami42ee", []any{"spam", 42}, false},
		{"list-nested", "li1eli2eee", []any{1, []any{2}}, false},
		{"list-unclosed", "l4:spam", nil, true},

		{"dict-empty", "de", map[string]any{}, false},
		{"dict-simple", "d3:bar4:spam3:fooi42ee",
			map[string]any{"bar": "spam", "foo": 42}, false},
		{"dict-unsorted-keys", "d3:foo3:bar3:bar3:fooe", nil, true},
		{"dict-unclosed", "d3:bar4:spam", nil, true},

		{"nested-list-2", "lli1eeli2eeli3eee",
			[]any{
				[]any{1},
				[]any{2},
				[]any{3},
			}, false},
		{"nested-list-3", "llli1eeli2eeli3eeee",
			[]any{
				[]any{
					[]any{1},
					[]any{2},
					[]any{3},
				},
			}, false},
		{"nested-dict-2", "d3:food3:bari2eee",
			map[string]any{"foo": map[string]any{"bar": 2}}, false},
		{"mix-list-dict-list", "l4:foodd3:barli1ei2ee3:baz4:spamee",
			[]any{
				"food",
				map[string]any{"bar": []any{1, 2}, "baz": "spam"},
			}, false},
		{"mix-dict-list-dict", "d4:landli3ee3:key5:valuee", nil, true},

		// ---- top-level errors ----
		{"empty-input", "", nil, true},
		{"invalid-prefix", "x", nil, true},
		{"trailing-data", "4:spamgarbage", nil, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bencode.Decode(tc.input)

			if tc.wantError {
				if err == nil {
					t.Errorf("Decode(%q): expected error, got %#v", tc.input, got)
				}
				return
			}

			if err != nil {
				t.Errorf("Decode(%q): unexpected error: %v", tc.input, err)
				return
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Decode(%q) = %#v; want %#v", tc.input, got, tc.want)
			}
		})
	}
}
