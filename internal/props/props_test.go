package props

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name, src string
		want      map[string]string
		wantErr   bool
	}{
		{"pairs", "a=1\nb.c = x=y\n", map[string]string{"a": "1", "b.c": "x=y"}, false},
		{"comments and blanks", "# c\n\na=1\n", map[string]string{"a": "1"}, false},
		{"no equals", "nope\n", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse([]byte(c.src))
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v", err)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("%s = %q", k, got[k])
				}
			}
		})
	}
}

func TestFormatSortsKeys(t *testing.T) {
	got := string(Format(map[string]string{"b": "2", "a": "1"}))
	if got != "a=1\nb=2\n" {
		t.Fatalf("got %q", got)
	}
}
