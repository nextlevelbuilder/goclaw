package edge

import "testing"

func TestSliderToString_PositivePercent(t *testing.T) {
	got := sliderToRateString(15)
	if got != "+15%" {
		t.Errorf("got %q, want +15%%", got)
	}
}

func TestSliderToString_NegativePercent(t *testing.T) {
	got := sliderToRateString(-25)
	if got != "-25%" {
		t.Errorf("got %q, want -25%%", got)
	}
}

func TestSliderToString_Zero(t *testing.T) {
	got := sliderToRateString(0)
	if got != "+0%" {
		t.Errorf("got %q, want +0%%", got)
	}
}

func TestSliderToString_PitchHz_Positive(t *testing.T) {
	got := sliderToPitchString(5)
	if got != "+5Hz" {
		t.Errorf("got %q, want +5Hz", got)
	}
}

func TestSliderToString_PitchHz_Negative(t *testing.T) {
	got := sliderToPitchString(-3)
	if got != "-3Hz" {
		t.Errorf("got %q, want -3Hz", got)
	}
}

func TestSliderToString_PitchHz_Zero(t *testing.T) {
	got := sliderToPitchString(0)
	if got != "+0Hz" {
		t.Errorf("got %q, want +0Hz", got)
	}
}

func TestStringToSlider_PositivePercent(t *testing.T) {
	got := rateStringToSlider("+15%")
	if got != 15 {
		t.Errorf("got %d, want 15", got)
	}
}

func TestStringToSlider_NegativePercent(t *testing.T) {
	got := rateStringToSlider("-25%")
	if got != -25 {
		t.Errorf("got %d, want -25", got)
	}
}

func TestStringToSlider_Zero(t *testing.T) {
	got := rateStringToSlider("+0%")
	if got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestStringToSlider_Hz(t *testing.T) {
	got := rateStringToSlider("+5Hz")
	if got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestStringToSlider_NegativeHz(t *testing.T) {
	got := rateStringToSlider("-3Hz")
	if got != -3 {
		t.Errorf("got %d, want -3", got)
	}
}

func TestStringToSlider_RoundTrip_Rate(t *testing.T) {
	for _, n := range []int{-50, -1, 0, 1, 50, 100} {
		s := sliderToRateString(n)
		back := rateStringToSlider(s)
		if back != n {
			t.Errorf("round-trip rate %d → %q → %d", n, s, back)
		}
	}
}

func TestStringToSlider_RoundTrip_Pitch(t *testing.T) {
	for _, n := range []int{-50, -1, 0, 1, 50} {
		s := sliderToPitchString(n)
		back := rateStringToSlider(s)
		if back != n {
			t.Errorf("round-trip pitch %d → %q → %d", n, s, back)
		}
	}
}

func TestStringToSlider_BadInput_Zero(t *testing.T) {
	got := rateStringToSlider("abc")
	if got != 0 {
		t.Errorf("got %d, want 0 for bad input", got)
	}
}

func TestStringToSlider_Empty_Zero(t *testing.T) {
	got := rateStringToSlider("")
	if got != 0 {
		t.Errorf("got %d, want 0 for empty input", got)
	}
}
