package bencode_test

import (
	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
	"reflect"
	"testing"
)

func TestUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		target  any
		want    any
		wantErr bool
	}{
		{
			name:   "empty string",
			input:  []byte("0:"),
			target: new(string),
			want:   "",
		},
		{
			name:   "simple string",
			input:  []byte("4:spam"),
			target: new(string),
			want:   "spam",
		},
		{
			name:   "string with spaces",
			input:  []byte("11:hello world"),
			target: new(string),
			want:   "hello world",
		},
		{
			name:   "unicode string",
			input:  []byte("5:café"),
			target: new(string),
			want:   "café",
		},
		{
			name:   "zero int",
			input:  []byte("i0e"),
			target: new(int),
			want:   int(0),
		},
		{
			name:   "positive int",
			input:  []byte("i42e"),
			target: new(int64),
			want:   int64(42),
		},
		{
			name:   "negative int",
			input:  []byte("i-7e"),
			target: new(int64),
			want:   int64(-7),
		},
		{
			name:   "large int",
			input:  []byte("i9223372036854775807e"),
			target: new(int64),
			want:   int64(9223372036854775807),
		},
		{
			name:   "int to different int types",
			input:  []byte("i42e"),
			target: new(int32),
			want:   int32(42),
		},
		{
			name:   "empty byte slice",
			input:  []byte("0:"),
			target: new([]byte),
			want:   []byte{},
		},
		{
			name:   "byte slice",
			input:  []byte("5:hello"),
			target: new([]byte),
			want:   []byte("hello"),
		},
		{
			name:   "binary byte slice",
			input:  []byte("3:\x00\xff\x7f"),
			target: new([]byte),
			want:   []byte{0x00, 0xff, 0x7f},
		},
		{
			name:   "empty slice",
			input:  []byte("le"),
			target: new([]string),
			want:   []string{},
		},
		{
			name:   "string slice",
			input:  []byte("l4:spam4:eggse"),
			target: new([]string),
			want:   []string{"spam", "eggs"},
		},
		{
			name:   "int slice",
			input:  []byte("li1ei2ei3ee"),
			target: new([]int),
			want:   []int{1, 2, 3},
		},
		{
			name:   "empty string map",
			input:  []byte("de"),
			target: new(map[string]string),
			want:   map[string]string{},
		},
		{
			name:   "simple string map",
			input:  []byte("d3:bar3:fooe"),
			target: new(map[string]string),
			want:   map[string]string{"bar": "foo"},
		},
		{
			name:   "multiple string map entries",
			input:  []byte("d3:bar3:foo3:baz3:quze"),
			target: new(map[string]string),
			want:   map[string]string{"bar": "foo", "baz": "quz"},
		},
		{
			name:   "string number to int",
			input:  []byte("2:42"),
			target: new(int),
			want:   int(42),
		},
		{
			name:   "string negative number to int",
			input:  []byte("2:-7"),
			target: new(int64),
			want:   int64(-7),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bencode.Unmarshal(tt.input, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				got := reflect.ValueOf(tt.target).Elem().Interface()
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("Unmarshal() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestUnmarshalStruct(t *testing.T) {
	type SimpleStruct struct {
		Name  string `bencode:"name"`
		Value int    `bencode:"value"`
	}

	type StructWithoutTags struct {
		Name  string
		Value int
	}

	type NestedStruct struct {
		Outer SimpleStruct `bencode:"outer"`
		Inner string       `bencode:"inner"`
	}

	type MixedStruct struct {
		Tagged   string `bencode:"custom_name"`
		Untagged string
		private  string
	}

	tests := []struct {
		name    string
		input   []byte
		target  any
		want    any
		wantErr bool
	}{
		{
			name:   "simple struct with tags",
			input:  []byte("d4:name4:test5:valuei42ee"),
			target: new(SimpleStruct),
			want:   SimpleStruct{Name: "test", Value: 42},
		},
		{
			name:   "struct without tags",
			input:  []byte("d4:Name4:test5:Valuei42ee"),
			target: new(StructWithoutTags),
			want:   StructWithoutTags{Name: "test", Value: 42},
		},
		{
			name:   "nested struct",
			input:  []byte("d5:inner5:inner5:outerd4:name5:outer5:valuei1eee"),
			target: new(NestedStruct),
			want: NestedStruct{
				Outer: SimpleStruct{Name: "outer", Value: 1},
				Inner: "inner",
			},
		},
		{
			name:   "mixed struct",
			input:  []byte("d8:Untagged8:untagged11:custom_name6:taggede"),
			target: new(MixedStruct),
			want:   MixedStruct{Tagged: "tagged", Untagged: "untagged"},
		},
		{
			name:   "struct with extra fields in input",
			input:  []byte("d5:extra5:value4:name4:test5:valuei42ee"),
			target: new(SimpleStruct),
			want:   SimpleStruct{Name: "test", Value: 42},
		},
		{
			name:   "struct with missing fields",
			input:  []byte("d4:name4:teste"),
			target: new(SimpleStruct),
			want:   SimpleStruct{Name: "test", Value: 0}, // zero value for missing field
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bencode.Unmarshal(tt.input, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				got := reflect.ValueOf(tt.target).Elem().Interface()
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("Unmarshal() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestUnmarshalErrors(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		target any
	}{
		{
			name:   "nil target",
			input:  []byte("4:test"),
			target: nil,
		},
		{
			name:   "non-pointer target",
			input:  []byte("4:test"),
			target: "string",
		},
		{
			name:   "nil pointer target",
			input:  []byte("4:test"),
			target: (*string)(nil),
		},
		{
			name:   "trailing data",
			input:  []byte("4:testextra"),
			target: new(string),
		},
		{
			name:   "empty input",
			input:  []byte(""),
			target: new(string),
		},
		{
			name:   "malformed string - no colon",
			input:  []byte("4test"),
			target: new(string),
		},
		{
			name:   "malformed string - short",
			input:  []byte("5:test"),
			target: new(string),
		},
		{
			name:   "malformed int - unterminated",
			input:  []byte("i42"),
			target: new(int),
		},
		{
			name:   "malformed int - empty",
			input:  []byte("ie"),
			target: new(int),
		},
		{
			name:   "malformed int - leading zero",
			input:  []byte("i042e"),
			target: new(int),
		},
		{
			name:   "malformed int - negative zero",
			input:  []byte("i-0e"),
			target: new(int),
		},
		{
			name:   "malformed list - unterminated",
			input:  []byte("l4:spam"),
			target: new([]string),
		},
		{
			name:   "malformed dict - unterminated",
			input:  []byte("d3:bar"),
			target: new(map[string]string),
		},
		{
			name:   "dict keys not in order",
			input:  []byte("d3:zoo3:baz3:bar3:fooe"),
			target: new(map[string]string),
		},
		{
			name:   "invalid prefix",
			input:  []byte("x123"),
			target: new(string),
		},
		{
			name:   "type mismatch - string to int",
			input:  []byte("4:test"),
			target: new(int),
		},
		{
			name:   "type mismatch - int to slice",
			input:  []byte("i42e"),
			target: new([]string),
		},
		{
			name:   "type mismatch - dict to string",
			input:  []byte("de"),
			target: new(string),
		},
		{
			name:   "unsupported target type",
			input:  []byte("4:test"),
			target: new(complex64),
		},
		{
			name:   "interface{} not supported",
			input:  []byte("4:test"),
			target: new(any),
		},
		{
			name:   "non-string dict key in bencode",
			input:  []byte("di42e4:teste"),
			target: new(map[string]string),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bencode.Unmarshal(tt.input, tt.target)
			if err == nil {
				t.Errorf("Unmarshal() expected error but got none")
			}
		})
	}
}

func TestParseValue(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		target    any
		wantValue any
		wantErr   bool
	}{
		{
			name:      "empty string",
			input:     []byte("0:"),
			target:    new(string),
			wantValue: "",
		},
		{
			name:      "simple string",
			input:     []byte("4:spam"),
			target:    new(string),
			wantValue: "spam",
		},
		{
			name:      "zero",
			input:     []byte("i0e"),
			target:    new(int64),
			wantValue: int64(0),
		},
		{
			name:      "positive int",
			input:     []byte("i42e"),
			target:    new(int64),
			wantValue: int64(42),
		},
		{
			name:      "negative int",
			input:     []byte("i-7e"),
			target:    new(int64),
			wantValue: int64(-7),
		},
		{
			name:      "empty list",
			input:     []byte("le"),
			target:    new([]string),
			wantValue: []string{},
		},
		{
			name:      "simple list",
			input:     []byte("l4:spam4:eggse"),
			target:    new([]string),
			wantValue: []string{"spam", "eggs"},
		},
		{
			name:      "empty dict",
			input:     []byte("de"),
			target:    new(map[string]string),
			wantValue: map[string]string{},
		},
		{
			name:      "simple dict",
			input:     []byte("d3:bar3:fooe"),
			target:    new(map[string]string),
			wantValue: map[string]string{"bar": "foo"},
		},
		{
			name:    "empty input",
			input:   []byte(""),
			target:  new(string),
			wantErr: true,
		},
		{
			name:    "invalid prefix",
			input:   []byte("x123"),
			target:  new(string),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bencode.Unmarshal(tt.input, tt.target)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseValue() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("parseValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			got := reflect.ValueOf(tt.target).Elem().Interface()
			if !reflect.DeepEqual(got, tt.wantValue) {
				t.Errorf("parseValue() = %v, want %v", got, tt.wantValue)
			}
		})
	}
}

func TestUnmarshalComplexData(t *testing.T) {
	type Info struct {
		Length      int64  `bencode:"length"`
		Name        string `bencode:"name"`
		PieceLength int64  `bencode:"piece length"`
		Pieces      []byte `bencode:"pieces"`
	}

	type Torrent struct {
		Announce string `bencode:"announce"`
		Info     Info   `bencode:"info"`
	}

	expected := Torrent{
		Announce: "http://tracker.example.com/",
		Info: Info{
			Length:      1048576,
			Name:        "test.txt",
			PieceLength: 262144,
			Pieces:      []byte("AAAAAAAAAAAAAAAAAAAA"),
		},
	}

	input, err := bencode.Marshal(expected)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var torrent Torrent
	err = bencode.Unmarshal(input, &torrent)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if !reflect.DeepEqual(torrent, expected) {
		t.Errorf("Unmarshal() = %+v, want %+v", torrent, expected)
	}
}
