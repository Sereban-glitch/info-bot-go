package session

import "testing"

func TestSignatureName(t *testing.T) {
	cases := []struct {
		name string
		p    Profile
		want string
	}{
		{"FullName priority", Profile{FullName: "Іван Петренко", FirstName: "Олена", LastName: "Коваль"}, "Іван Петренко"},
		{"parts fallback", Profile{FirstName: "Іван", LastName: "Петренко", MiddleName: "Петрович"}, "Петренко Іван Петрович"},
		{"first name only", Profile{FirstName: "Іван"}, "Іван"},
		{"empty profile", Profile{}, ""},
		{"whitespace FullName", Profile{FullName: "   "}, ""},
	}
	for _, tc := range cases {
		if got := SignatureName(tc.p); got != tc.want {
			t.Errorf("%s: SignatureName=%q, want %q", tc.name, got, tc.want)
		}
	}
}
