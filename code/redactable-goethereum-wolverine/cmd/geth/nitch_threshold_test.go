package main

import "testing"

func TestParseNitchThreshold(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		degree   int
		required int
		wantErr  bool
	}{
		{name: "legacy default", degree: 2, required: 3},
		{name: "nine share quorum", raw: "9", degree: 8, required: 9},
		{name: "one share quorum", raw: "1", degree: 0, required: 1},
		{name: "trim spaces", raw: " 9 ", degree: 8, required: 9},
		{name: "zero", raw: "0", wantErr: true},
		{name: "negative", raw: "-1", wantErr: true},
		{name: "not a number", raw: "nine", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			degree, required, err := parseNitchThreshold(test.raw)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseNitchThreshold(%q) error = %v, wantErr %v", test.raw, err, test.wantErr)
			}
			if !test.wantErr && (degree != test.degree || required != test.required) {
				t.Fatalf("parseNitchThreshold(%q) = (%d, %d), want (%d, %d)",
					test.raw, degree, required, test.degree, test.required)
			}
		})
	}
}
