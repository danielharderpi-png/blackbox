package diff

import (
	"math/rand"
	"testing"
)

func lcsLen(a, b []string) int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}
	return dp[len(a)][len(b)]
}

func TestLinesMinimalAndConsistent(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alpha := []string{"a", "b", "c", "d", "e"}
	gen := func() []string {
		s := make([]string, rng.Intn(15))
		for i := range s {
			s[i] = alpha[rng.Intn(len(alpha))]
		}
		return s
	}
	for i := 0; i < 2000; i++ {
		a, b := gen(), gen()
		add, rem, approx := Lines(a, b)
		if approx {
			t.Fatal("unexpected approx")
		}
		l := lcsLen(a, b)
		if len(add) != len(b)-l || len(rem) != len(a)-l {
			t.Fatalf("a=%v b=%v got +%d -%d want +%d -%d", a, b, len(add), len(rem), len(b)-l, len(a)-l)
		}
		// Unmarked lines must form an identical sequence.
		skipA, skipB := map[int]bool{}, map[int]bool{}
		for _, x := range rem {
			skipA[x] = true
		}
		for _, x := range add {
			skipB[x] = true
		}
		var ka, kb []string
		for i, s := range a {
			if !skipA[i] {
				ka = append(ka, s)
			}
		}
		for i, s := range b {
			if !skipB[i] {
				kb = append(kb, s)
			}
		}
		if len(ka) != len(kb) {
			t.Fatalf("kept mismatch %v %v", ka, kb)
		}
		for i := range ka {
			if ka[i] != kb[i] {
				t.Fatalf("kept mismatch %v %v", ka, kb)
			}
		}
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		rel  string
		pats []string
		want bool
	}{
		{"node_modules/x/i.js", []string{"node_modules/**"}, true},
		{"a/node_modules", []string{"node_modules/**"}, true},
		{"secrets/key.txt", []string{"secrets/**"}, true},
		{"a/secrets/key.txt", []string{"/secrets/**"}, false},
		{"certs/a.pem", []string{"**/*.pem"}, true},
		{".env.local", []string{".env.*"}, true},
		{"src/main.go", []string{".env", "dist/**"}, false},
	}
	for _, c := range cases {
		if got := Match(c.rel, c.pats); got != c.want {
			t.Errorf("Match(%q,%v)=%v want %v", c.rel, c.pats, got, c.want)
		}
	}
}
