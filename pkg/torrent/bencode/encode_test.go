package bencode_test

import (
	"bytes"
	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
	"reflect"
	"testing"
)

func TestMarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    []byte
		wantErr bool
	}{
		{
			name:  "empty string",
			input: "",
			want:  []byte("0:"),
		},
		{
			name:  "simple string",
			input: "spam",
			want:  []byte("4:spam"),
		},
		{
			name:  "string with spaces",
			input: "hello world",
			want:  []byte("11:hello world"),
		},
		{
			name:  "unicode string",
			input: "café",
			want:  []byte("5:café"),
		},
		{
			name:  "binary string",
			input: string([]byte{0x00, 0xff, 0x7f}),
			want:  []byte("3:\x00\xff\x7f"),
		},
		{
			name:  "zero int",
			input: int(0),
			want:  []byte("i0e"),
		},
		{
			name:  "positive int",
			input: int(42),
			want:  []byte("i42e"),
		},
		{
			name:  "negative int",
			input: int(-7),
			want:  []byte("i-7e"),
		},
		{
			name:  "int64 max",
			input: int64(9223372036854775807),
			want:  []byte("i9223372036854775807e"),
		},
		{
			name:  "int64 min",
			input: int64(-9223372036854775808),
			want:  []byte("i-9223372036854775808e"),
		},
		{
			name:  "uint64",
			input: uint64(18446744073709551615),
			want:  []byte("i18446744073709551615e"),
		},
		{
			name:  "int8",
			input: int8(-128),
			want:  []byte("i-128e"),
		},
		{
			name:  "uint8",
			input: uint8(255),
			want:  []byte("i255e"),
		},
		{
			name:  "bool true",
			input: true,
			want:  []byte("i1e"),
		},
		{
			name:  "bool false",
			input: false,
			want:  []byte("i0e"),
		},
		{
			name:  "empty byte slice",
			input: []byte{},
			want:  []byte("0:"),
		},
		{
			name:  "byte slice",
			input: []byte("hello"),
			want:  []byte("5:hello"),
		},
		{
			name:  "binary byte slice",
			input: []byte{0x00, 0xff, 0x7f},
			want:  []byte("3:\x00\xff\x7f"),
		},
		{
			name:  "empty slice",
			input: []any{},
			want:  []byte("le"),
		},
		{
			name:  "slice with string",
			input: []string{"spam", "eggs"},
			want:  []byte("l4:spam4:eggse"),
		},
		{
			name:  "slice with mixed types",
			input: []any{"spam", int(42), []byte("test")},
			want:  []byte("l4:spami42e4:teste"),
		},
		{
			name:  "nested slice",
			input: []any{[]string{"inner"}, "outer"},
			want:  []byte("ll5:innere5:outere"),
		},
		{
			name:  "int array",
			input: [3]int{1, 2, 3},
			want:  []byte("li1ei2ei3ee"),
		},
		{
			name:  "string array",
			input: [2]string{"foo", "bar"},
			want:  []byte("l3:foo3:bare"),
		},
		{
			name:  "empty map",
			input: map[string]any{},
			want:  []byte("de"),
		},
		{
			name:  "simple map",
			input: map[string]any{"bar": "foo"},
			want:  []byte("d3:bar3:fooe"),
		},
		{
			name:  "map with multiple keys (sorted)",
			input: map[string]any{"zebra": "animal", "apple": "fruit"},
			want:  []byte("d5:apple5:fruit5:zebra6:animale"),
		},
		{
			name:  "nested map",
			input: map[string]any{"outer": map[string]any{"inner": "value"}},
			want:  []byte("d5:outerd5:inner5:valueee"),
		},
		{
			name:  "map with different value types",
			input: map[string]any{"str": "text", "num": int(42), "bool": true},
			want:  []byte("d4:booli1e3:numi42e3:str4:texte"),
		},
		{
			name:  "nil pointer",
			input: (*string)(nil),
			want:  []byte("0:"),
		},
		{
			name:  "nil interface",
			input: (any)(nil),
			want:  []byte("0:"),
		},
		{
			name:  "pointer to string",
			input: func() *string { s := "test"; return &s }(),
			want:  []byte("4:test"),
		},
		{
			name:    "unsupported type - function",
			input:   func() {},
			wantErr: true,
		},
		{
			name:    "unsupported type - channel",
			input:   make(chan int),
			wantErr: true,
		},
		{
			name:    "map with non-string keys",
			input:   map[int]string{1: "value"},
			wantErr: true,
		},
		{
			name:    "unsupported type - complex",
			input:   complex(1, 2),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bencode.Marshal(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Marshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !bytes.Equal(got, tt.want) {
				t.Errorf("Marshal() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMarshalStruct(t *testing.T) {
	type SimpleStruct struct {
		Name  string `bencode:"name"`
		Value int    `bencode:"value"`
	}

	type StructWithSkipField struct {
		Public  string `bencode:"public"`
		Skip    string `bencode:"-"`
		private string
	}

	type StructNoTags struct {
		Name  string
		Value int
	}

	type NestedStruct struct {
		Outer SimpleStruct `bencode:"outer"`
		Inner string       `bencode:"inner"`
	}

	tests := []struct {
		name  string
		input any
		want  []byte
	}{
		{
			name:  "simple struct with tags",
			input: SimpleStruct{Name: "test", Value: 42},
			want:  []byte("d4:name4:test5:valuei42ee"),
		},
		{
			name:  "struct with skip field",
			input: StructWithSkipField{Public: "visible", Skip: "hidden", private: "invisible"},
			want:  []byte("d6:public7:visiblee"),
		},
		{
			name:  "struct without tags",
			input: StructNoTags{Name: "test", Value: 42},
			want:  []byte("d4:Name4:test5:Valuei42ee"),
		},
		{
			name:  "nested struct",
			input: NestedStruct{Outer: SimpleStruct{Name: "outer", Value: 1}, Inner: "inner"},
			want:  []byte("d5:inner5:inner5:outerd4:name5:outer5:valuei1eee"),
		},
		{
			name:  "empty struct",
			input: struct{}{},
			want:  []byte("de"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bencode.Marshal(tt.input)
			if err != nil {
				t.Errorf("Marshal() error = %v", err)
				return
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Marshal() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		original := "hello world"
		encoded, err := bencode.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}

		var decoded string
		err = bencode.Unmarshal(encoded, &decoded)
		if err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		if decoded != original {
			t.Errorf("Round trip failed: got %v, want %v", decoded, original)
		}
	})

	t.Run("int", func(t *testing.T) {
		original := int(42)
		encoded, err := bencode.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}

		var decoded int64
		err = bencode.Unmarshal(encoded, &decoded)
		if err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		if decoded != int64(original) {
			t.Errorf("Round trip failed: got %v, want %v", decoded, int64(original))
		}
	})

	t.Run("negative int", func(t *testing.T) {
		original := int(-7)
		encoded, err := bencode.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}

		var decoded int64
		err = bencode.Unmarshal(encoded, &decoded)
		if err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		if decoded != int64(original) {
			t.Errorf("Round trip failed: got %v, want %v", decoded, int64(original))
		}
	})

	t.Run("slice", func(t *testing.T) {
		original := []string{"a", "b", "c"}
		encoded, err := bencode.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}

		var decoded []string
		err = bencode.Unmarshal(encoded, &decoded)
		if err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		if !reflect.DeepEqual(decoded, original) {
			t.Errorf("Round trip failed: got %v, want %v", decoded, original)
		}
	})

	t.Run("map", func(t *testing.T) {
		original := map[string]string{"key": "value", "num": "123"}
		encoded, err := bencode.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}

		var decoded map[string]string
		err = bencode.Unmarshal(encoded, &decoded)
		if err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		if !reflect.DeepEqual(decoded, original) {
			t.Errorf("Round trip failed: got %v, want %v", decoded, original)
		}
	})

	t.Run("byte slice", func(t *testing.T) {
		original := []byte("binary data")
		encoded, err := bencode.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}

		var decoded []byte
		err = bencode.Unmarshal(encoded, &decoded)
		if err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		if !reflect.DeepEqual(decoded, original) {
			t.Errorf("Round trip failed: got %v, want %v", decoded, original)
		}
	})

	t.Run("bool true", func(t *testing.T) {
		original := true
		encoded, err := bencode.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}

		var decoded int64
		err = bencode.Unmarshal(encoded, &decoded)
		if err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		expected := int64(1)
		if decoded != expected {
			t.Errorf("Round trip failed: got %v, want %v", decoded, expected)
		}
	})

	t.Run("bool false", func(t *testing.T) {
		original := false
		encoded, err := bencode.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}

		var decoded int64
		err = bencode.Unmarshal(encoded, &decoded)
		if err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}

		expected := int64(0)
		if decoded != expected {
			t.Errorf("Round trip failed: got %v, want %v", decoded, expected)
		}
	})
}
