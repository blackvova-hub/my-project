package scannerapi

import "testing"

func TestAlertWindowBoundariesSupport24HoursInProduction(t *testing.T) {
	tests := []struct {
		value int
		want  bool
	}{{0, false}, {1, true}, {60, true}, {720, true}, {721, true}, {1440, true}, {1441, false}}
	for _, test := range tests {
		if got := isValidAlertWindowMinutes(test.value); got != test.want {
			t.Fatalf("isValidAlertWindowMinutes(%d) = %v, want %v", test.value, got, test.want)
		}
	}
}
